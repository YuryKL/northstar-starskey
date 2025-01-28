package index

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"polaris/web/components"

	"github.com/delaneyj/toolbelt/embeddednats"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/nats-io/nats.go"
	datastar "github.com/starfederation/datastar/sdk/go"
	"github.com/starskey-io/starskey"
)

func SetupIndexRoute(router chi.Router, store sessions.Store, skey *starskey.Starskey, ns *embeddednats.Server) error {
	nc, err := ns.Client()
	if err != nil {
		return fmt.Errorf("failed to create NATS client: %w", err)
	}

	saveMVC := func(_ context.Context, sessionID string, mvc *components.TodoMVC) error {
		b, err := json.Marshal(mvc)
		if err != nil {
			return fmt.Errorf("failed to marshal mvc: %w", err)
		}
		if err := skey.Put([]byte(sessionID), b); err != nil {
			return fmt.Errorf("failed to put key value: %w", err)
		}
		// Publish update notification
		if err := nc.Publish("todos."+sessionID+".updated", b); err != nil {
			return fmt.Errorf("failed to publish update: %w", err)
		}
		return nil
	}

	resetMVC := func(mvc *components.TodoMVC) {
		mvc.Mode = components.TodoViewModeAll
		mvc.Todos = []*components.Todo{
			{Text: "Learn a backend language", Completed: true},
			{Text: "Learn Datastar", Completed: false},
			{Text: "Create Hypermedia", Completed: false},
			{Text: "???", Completed: false},
			{Text: "Profit", Completed: false},
		}
		mvc.EditingIdx = -1
	}

	mvcSession := func(w http.ResponseWriter, r *http.Request) (string, *components.TodoMVC, error) {
		ctx := r.Context()
		sessionID, err := upsertSessionID(store, r, w)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get session id: %w", err)
		}

		mvc := &components.TodoMVC{}
		result, err := skey.Get([]byte(sessionID))
		if err != nil || len(result) == 0 {
			slog.Info("initializing new mvc", "error", err)
			resetMVC(mvc)
			if err := saveMVC(ctx, sessionID, mvc); err != nil {
				return "", nil, fmt.Errorf("failed to save mvc: %w", err)
			}
		} else {
			if err := json.Unmarshal(result, mvc); err != nil {
				slog.Error("failed to unmarshal mvc, resetting", "error", err)
				resetMVC(mvc)
				if err := saveMVC(ctx, sessionID, mvc); err != nil {
					return "", nil, fmt.Errorf("failed to save mvc: %w", err)
				}
			}
		}
		return sessionID, mvc, nil
	}

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		Index("HYPERMEDIA RULES").Render(r.Context(), w)
	})

	router.Route("/api", func(apiRouter chi.Router) {
		apiRouter.Route("/todos", func(todosRouter chi.Router) {
			todosRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
				sessionID, mvc, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				sse := datastar.NewSSE(w, r)

				// Initial render
				c := components.TodosMVCView(mvc)
				if err := sse.MergeFragmentTempl(c); err != nil {
					sse.ConsoleError(err)
					return
				}

				// Subscribe to updates
				sub, err := nc.Subscribe("todos."+sessionID+".updated", func(msg *nats.Msg) {
					if err := json.Unmarshal(msg.Data, mvc); err != nil {
						sse.ConsoleError(err)
						return
					}
					c := components.TodosMVCView(mvc)
					if err := sse.MergeFragmentTempl(c); err != nil {
						sse.ConsoleError(err)
						return
					}
				})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				defer sub.Unsubscribe()

				// Wait for client disconnect
				<-r.Context().Done()
			})

			todosRouter.Put("/reset", func(w http.ResponseWriter, r *http.Request) {
				sessionID, mvc, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				resetMVC(mvc)
				if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			todosRouter.Put("/cancel", func(w http.ResponseWriter, r *http.Request) {
				sessionID, mvc, err := mvcSession(w, r)
				sse := datastar.NewSSE(w, r)
				if err != nil {
					sse.ConsoleError(err)
					return
				}

				mvc.EditingIdx = -1
				if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
					sse.ConsoleError(err)
					return
				}
			})

			todosRouter.Put("/mode/{mode}", func(w http.ResponseWriter, r *http.Request) {
				sessionID, mvc, err := mvcSession(w, r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				modeStr := chi.URLParam(r, "mode")
				modeRaw, err := strconv.Atoi(modeStr)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				mode := components.TodoViewMode(modeRaw)
				if mode < components.TodoViewModeAll || mode > components.TodoViewModeCompleted {
					http.Error(w, "invalid mode", http.StatusBadRequest)
					return
				}

				mvc.Mode = mode
				if err := saveMVC(r.Context(), sessionID, mvc); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			todosRouter.Route("/{idx}", func(todoRouter chi.Router) {
				routeIndex := func(w http.ResponseWriter, r *http.Request) (int, error) {
					idx := chi.URLParam(r, "idx")
					i, err := strconv.Atoi(idx)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return 0, err
					}
					return i, nil
				}

				todoRouter.Post("/toggle", func(w http.ResponseWriter, r *http.Request) {
					sessionID, mvc, err := mvcSession(w, r)
					sse := datastar.NewSSE(w, r)
					if err != nil {
						sse.ConsoleError(err)
						return
					}

					i, err := routeIndex(w, r)
					if err != nil {
						sse.ConsoleError(err)
						return
					}

					if i < 0 {
						setCompletedTo := false
						for _, todo := range mvc.Todos {
							if !todo.Completed {
								setCompletedTo = true
								break
							}
						}
						for _, todo := range mvc.Todos {
							todo.Completed = setCompletedTo
						}
					} else {
						todo := mvc.Todos[i]
						todo.Completed = !todo.Completed
					}

					saveMVC(r.Context(), sessionID, mvc)
				})

				todoRouter.Route("/edit", func(editRouter chi.Router) {
					editRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
						sessionID, mvc, err := mvcSession(w, r)
						if err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}

						i, err := routeIndex(w, r)
						if err != nil {
							return
						}

						mvc.EditingIdx = i
						saveMVC(r.Context(), sessionID, mvc)
					})

					editRouter.Put("/", func(w http.ResponseWriter, r *http.Request) {
						type Store struct {
							Input string `json:"input"`
						}
						store := &Store{}

						if err := datastar.ReadSignals(r, store); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}

						if store.Input == "" {
							return
						}

						sessionID, mvc, err := mvcSession(w, r)
						if err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}

						i, err := routeIndex(w, r)
						if err != nil {
							return
						}

						if i >= 0 {
							mvc.Todos[i].Text = store.Input
						} else {
							mvc.Todos = append(mvc.Todos, &components.Todo{
								Text:      store.Input,
								Completed: false,
							})
						}
						mvc.EditingIdx = -1

						saveMVC(r.Context(), sessionID, mvc)
					})
				})

				todoRouter.Delete("/", func(w http.ResponseWriter, r *http.Request) {
					i, err := routeIndex(w, r)
					if err != nil {
						return
					}

					sessionID, mvc, err := mvcSession(w, r)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}

					if i >= 0 {
						mvc.Todos = append(mvc.Todos[:i], mvc.Todos[i+1:]...)
					} else {
						var activeTodos []*components.Todo
						for _, todo := range mvc.Todos {
							if !todo.Completed {
								activeTodos = append(activeTodos, todo)
							}
						}
						mvc.Todos = activeTodos
					}
					saveMVC(r.Context(), sessionID, mvc)
				})
			})
		})
	})

	return nil
}

func upsertSessionID(store sessions.Store, r *http.Request, w http.ResponseWriter) (string, error) {
	sess, err := store.Get(r, "connections")
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}

	id, ok := sess.Values["id"].(string)
	if !ok {
		id = fmt.Sprintf("session-%d", len(sess.Values))
		sess.Values["id"] = id
		if err := sess.Save(r, w); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
	}
	return id, nil
}
