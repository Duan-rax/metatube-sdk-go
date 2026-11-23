# MetaTube SDK Go

[![Build Status](https://img.shields.io/github/actions/workflow/status/metatube-community/metatube-sdk-go/docker.yml?branch=main&style=flat-square&logo=github-actions)](https://github.com/metatube-community/metatube-sdk-go/actions/workflows/release.yml)
[![Go Linter](https://img.shields.io/github/actions/workflow/status/metatube-community/metatube-sdk-go/linter.yml?style=flat-square&branch=main&logo=githubactions&label=golangci-lint)](https://github.com/metatube-community/metatube-sdk-go/actions/workflows/linter.yml)
[![Require Go Version](https://img.shields.io/badge/go-%3E%3D1.25-30dff3?style=flat-square&logo=go)](https://github.com/metatube-community/metatube-sdk-go/blob/main/go.mod)
[![GitHub License](https://img.shields.io/github/license/metatube-community/metatube-sdk-go?color=e4682a&logo=apache&style=flat-square)](https://github.com/metatube-community/metatube-sdk-go/blob/main/LICENSE)
[![Tag](https://img.shields.io/github/v/tag/metatube-community/metatube-sdk-go?color=%23ff8936&logo=fitbit&style=flat-square)](https://github.com/metatube-community/metatube-sdk-go/tags)

Metadata Tube SDK in Golang.

## Contents

- [MetaTube SDK Go](#metatube-sdk-go)
    - [Contents](#contents)
    - [Features](#features)
    - [Installation](#installation)
- [FC2 providers](#fc2-providers)
    - [Credits](#credits)
    - [License](#license)

## Features

- Supported platforms
    - Linux
    - Darwin
    - Windows
    - BSD(s)
- Supported Databases
    - [SQLite](https://gitlab.com/cznic/sqlite)
    - [PostgreSQL](https://github.com/jackc/pgx)
- Image processing
    - Auto cropping
    - Badge support
    - Face detection
    - Image hashing
- RESTful API
- 20+ providers
- Text translation

`JavDB` provides basic FC2 metadata from public search results as a lower-priority
alternative to `FC2`, `fc2hub`, and `FC2PPVDB`. It matches the exact FC2 number and
provides the title, cover, release date, and rating without requiring a login.
Actor names and other detail-page fields are not available through this provider.
Its IDs are numeric FC2 IDs; supported URLs are public search URLs such as
`https://javdb.com/search?q=FC2-3119569&f=all`.

## Installation

To install this package, you first need [Go](https://golang.org/) installed (**go1.25+ is required**), then you can use
the below Go command to install SDK.

```sh
go get -u github.com/metatube-community/metatube-sdk-go
```

## FC2 providers

The `FC2PPVDB` provider reads public article pages on `fc2cmadb.com`.
Existing numeric IDs and article links on `fc2ppvdb.com` resolve through the same provider.
It reads the title, cover, release date, runtime, publisher and tags when present in the page.

`JavDB` matches the exact FC2 number in the search results. Public search is the default.
To use the results visible to your signed-in account, configure the value of the JavDB
`Cookie` request header, such as `name=value; other=value`. Supply the header value alone.
Use either `MT_MOVIE_PROVIDER_JAVDB__COOKIE` or `MT_MOVIE_PROVIDER_JAVDB__COOKIE_FILE`,
then restart the MetaTube backend to activate the configuration. Cookie file updates
take effect on the next uncached lookup. An empty file uses anonymous search.
Keep the cookie out of the repository and logs.
MetaTube uses it only for HTTPS searches on `javdb.com`. Results can contain the title,
cover, release date and score. This integration reads the search cards.

For Docker Compose, put the value on one line in a private file and add the following
settings to the existing `metatube` service:

```yaml
services:
  metatube:
    environment:
      MT_MOVIE_PROVIDER_JAVDB__COOKIE_FILE: /run/secrets/javdb_cookie
    secrets:
      - javdb_cookie

secrets:
  javdb_cookie:
    file: /path/to/private/javdb_cookie
```

For a co-located Emby plugin that replaces its Cookie file atomically, mount the
dedicated secret directory read-only and point `MT_MOVIE_PROVIDER_JAVDB__COOKIE_FILE`
at the file inside that directory. Mounting the directory lets MetaTube see replacements.

`FC2DB` is also available through the standard provider registry. To inspect an
individual source from the backend, request `/v1/movies/FC2PPVDB/<number>`,
`/v1/movies/FC2DB/<number>` or `/v1/movies/JavDB/<number>`.
For a deployment that needs a trusted proxy for the official FC2 scraper, set
`MT_MOVIE_PROVIDER_FC2__PROXY` to the proxy URL and leave unrelated providers on
their normal network path.

## Credits

| Library														                                           | Description																						                                                                    |
|-----------------------------------------------------------------|------------------------------------------------------------------------------------------------------|
| [gocolly/colly](https://github.com/gocolly/colly)			            | Elegant Scraper and Crawler Framework for Golang													                                        |
| [gin-gonic/gin](https://github.com/gin-gonic/gin)			            | Gin is a HTTP web framework written in Go															                                             |
| [gorm.io/gorm](https://gorm.io/)								                        | The fantastic ORM library for Golang																                                                 |
| [esimov/pigo](https://github.com/esimov/pigo)				               | Fast face detection, pupil/eyes localization and facial landmark points detection library in pure Go |
| [robertkrimen/otto](https://github.com/robertkrimen/otto)       | A JavaScript interpreter in Go (golang)                                                              |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)		         | Package sqlite is a CGo-free port of SQLite/SQLite3												                                      |
| [corona10/goimagehash](https://github.com/corona10/goimagehash) | Go Perceptual image hashing package																                                                  |
| [antchfx/xpath](https://github.com/antchfx/xpath)			            | XPath package for Golang, supports HTML, XML, JSON document query									                           |
| [gen2brain/jpegli](https://github.com/gen2brain/jpegli)         | Go encoder/decoder for JPEG based on jpegli                                                          |

## License

[Apache-2.0 License](https://github.com/metatube-community/metatube-sdk-go/blob/main/LICENSE)
