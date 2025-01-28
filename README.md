# Northstar Starskey

A modified version of the [Northstar](https://github.com/zangster300/northstar) template built with [Starskey](https://starskey.io/).

## Tech Stack

- [Go](https://go.dev/doc/) 
- [NATS](https://docs.nats.io/) 
- [Datastar](https://github.com/starfederation/datastar) 
- [Templ](https://templ.guide/) 
- [Starskey](https://starskey.io/) 
- [Tailwind CSS](https://tailwindcss.com/) + [DaisyUI](https://daisyui.com/) 
- [esbuild](https://esbuild.github.io/) 

## Getting Started

1. Clone the repository:
```shell
git clone https://github.com/YuryKL/northstar-starskey.git
cd northstar-starskey
```

2. Install dependencies:
```shell
pnpm install
go mod tidy
```

## Development

Start the development server with live reload:
```shell
task live
```
This will start the server at [`http://localhost:7331`](http://localhost:7331)

### Debugging

Launch a debugging session:
```shell
task debug
```

