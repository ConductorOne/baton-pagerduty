![Baton Logo](./docs/images/baton-logo.png)

# `baton-pagerduty` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-pagerduty.svg)](https://pkg.go.dev/github.com/conductorone/baton-pagerduty) ![main ci](https://github.com/conductorone/baton-pagerduty/actions/workflows/main.yaml/badge.svg)

`baton-pagerduty` is a connector for PagerDuty built using the [Baton SDK](https://github.com/conductorone/baton-sdk). It communicates with the PagerDuty User provisioning API to sync data about teams, users and their roles.

Check out [Baton](https://github.com/conductorone/baton) to learn more about the project in general.

# Prerequisites

To work with the connector, you need to obtain API access token from PagerDuty. To directly create an API access token, you need to have an admin or owner account in PagerDuty, but you can also ask admin users to provide one.

There are two ways to obtain an API key:
- Create token by going to the top menu bar, selecting `Integrations` -> `API Access Keys` or
- Create user-scoped token by hovering over the profile icon in the top right corner and choosing `My Profile` -> `User Settings` -> `Create API User Token` 

Be aware that to sync all the users, teams and roles associated with them with user-scoped token, you can't have restricted access role for that user.

# Getting Started

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-pagerduty

BATON_TOKEN=token baton-pagerduty
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_TOKEN=token ghcr.io/conductorone/baton-pagerduty:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-pagerduty/cmd/baton-pagerduty@main

BATON_TOKEN=token baton-pagerduty
baton resources
```

# Data Model

`baton-pagerduty` will pull down information about the following PagerDuty resources:

- Users
- Teams

By default, `baton-pagerduty` will sync information only from account based on provided credential.

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually building spreadsheets. We welcome contributions, and ideas, no matter how small -- our goal is to make identity and permissions sprawl less painful for everyone. If you have questions, problems, or ideas: Please open a Github Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-pagerduty` Command Line Usage

```
baton-pagerduty

Usage:
  baton-pagerduty [flags]
  baton-pagerduty [command]

Available Commands:
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --client-id string       The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string   The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string            The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                   help for baton-pagerduty
      --log-format string      The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string       The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
      --token string           The PagerDuty access token used to connect to the PagerDuty API. ($BATON_TOKEN)
  -v, --version                version for baton-pagerduty

Use "baton-pagerduty [command] --help" for more information about a command.
```
