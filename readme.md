# `panascais/ci-rust`

[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/panascais-docker/ci-rust/main.yml?branch=master&style=flat-square)](https://github.com/panascais-docker/ci-rust/actions?query=workflow%3Amain)
[![Docker Pulls](https://img.shields.io/docker/pulls/panascais/ci-rust.svg?style=flat-square)](https://hub.docker.com/r/panascais/ci-rust)
[![Docker Stars](https://img.shields.io/docker/stars/panascais/ci-rust.svg?style=flat-square)](https://hub.docker.com/r/panascais/ci-rust)
[![Docker Image Size](https://img.shields.io/docker/image-size/panascais/ci-rust.svg?style=flat-square)](https://hub.docker.com/r/panascais/ci-rust)
[![License](https://img.shields.io/github/license/panascais-docker/ci-rust.svg?style=flat-square)](https://hub.docker.com/r/panascais/ci-rust)

Rust CI images for `linux/amd64` and `linux/arm64`. Each image installs the toolchain with [`rustup`](https://rustup.rs) on alpine 3.24 or Debian trixie. They come with `cargo nextest`, `cargo deny`, `just` and `sccache`, and `cargo build --target <arch>-unknown-linux-musl` produces a static binary without extra setup.

| **Tag:**      | **Command:**                           | **Rust Version:** | **Variants:**                 |
| ------------- | -------------------------------------- | ----------------- | ----------------------------- |
| `latest`      | `docker pull panascais/ci-rust`        | `1.98.x`          | **alpine**, trixie            |
| `1.98`, `1`   | `docker pull panascais/ci-rust:1.98`   | `1.98.x`          | **alpine**, trixie            |
| `1.97`        | `docker pull panascais/ci-rust:1.97`   | `1.97.x`          | **alpine**, trixie            |
| `1.96`        | `docker pull panascais/ci-rust:1.96`   | `1.96.x`          | **alpine**, trixie            |
| `1.95`        | `docker pull panascais/ci-rust:1.95`   | `1.95.x`          | **alpine**, trixie            |
| `1.94`        | `docker pull panascais/ci-rust:1.94`   | `1.94.x`          | **alpine**, trixie            |
| `1.93`        | `docker pull panascais/ci-rust:1.93`   | `1.93.x`          | **alpine**, trixie            |
| `1.92`        | `docker pull panascais/ci-rust:1.92`   | `1.92.x`          | **alpine**, trixie            |
| `1.91`        | `docker pull panascais/ci-rust:1.91`   | `1.91.x`          | **alpine**, trixie            |
| `1.90`        | `docker pull panascais/ci-rust:1.90`   | `1.90.x`          | **alpine**, trixie            |
| `1.89`        | `docker pull panascais/ci-rust:1.89`   | `1.89.x`          | **alpine**, trixie            |
| `1.88`        | `docker pull panascais/ci-rust:1.88`   | `1.88.x`          | **alpine**, trixie            |
| `1.87`        | `docker pull panascais/ci-rust:1.87`   | `1.87.x`          | **alpine**, trixie            |
| `1.86`        | `docker pull panascais/ci-rust:1.86`   | `1.86.x`          | **alpine**, trixie            |
| `1.85`        | `docker pull panascais/ci-rust:1.85`   | `1.85.x`          | **alpine**, trixie            |

Tags are built as `<version>[-<variant>]`:

| **Part:** | **Values:**                                                | **When left out:** |
| --------- | ---------------------------------------------------------- | ------------------ |
| version   | `latest`, a major `1`, a line `1.98`, a patch `1.98.1`     | always required    |
| variant   | `-alpine`, `-trixie`                                       | alpine             |

For example `1.98` and `1.98-alpine` are Rust 1.98 on alpine, `1.98.1-trixie` is Rust 1.98.1 on Debian trixie and `latest-trixie` is the newest Rust on Debian trixie. The pipeline builds every line from 1.85 on, each in both variants. There are no nightly images, only stable releases.

## Included tools

| **Name:**       | **GitHub:**                                                                      |
| --------------- | -------------------------------------------------------------------------------- |
| `cargo-deny`    | [github.com/EmbarkStudios/cargo-deny](https://github.com/EmbarkStudios/cargo-deny) |
| `cargo-nextest` | [github.com/nextest-rs/nextest](https://github.com/nextest-rs/nextest)           |
| `just`          | [github.com/casey/just](https://github.com/casey/just)                           |
| `rustup`        | [github.com/rust-lang/rustup](https://github.com/rust-lang/rustup)               |
| `sccache`       | [github.com/mozilla/sccache](https://github.com/mozilla/sccache)                 |

The tools are release binaries in `/usr/local/cargo/bin`, each checked against a pinned sha256. `rustup` installs the toolchain with `clippy`, `rustfmt` and `rust-src`. For crates that compile C, like `ring`, `aws-lc-sys` or `zstd-sys`, there is a C toolchain with `clang`, `cmake` and `pkg-config`. `git`, `curl` and `openssh-client` cover git dependencies.

There is no `libssl-dev`, so crates have to use rustls. There is no Docker CLI either, because testcontainers talks to the Docker socket directly.

## Usage

```yaml
image: quay.io/panascais/ci-rust:1.98

test:
    script:
        - cargo nextest run
```

The images run as root and have no entrypoint. They set `RUSTUP_HOME=/usr/local/rustup`, `CARGO_HOME=/usr/local/cargo`, `CARGO_INCREMENTAL=0` and `CARGO_NET_GIT_FETCH_WITH_CLI=true`, with `/usr/local/cargo/bin` on `PATH`. A project that points `CARGO_HOME` into its checkout to cache the registry keeps every tool, because `PATH` names `/usr/local/cargo/bin` directly.

### Static binaries

```sh
cargo build --release --target "$(uname -m)-unknown-linux-musl"
```

The result has no dynamic loader and runs in a `FROM scratch` image. On alpine musl is the host target. The Debian variants add the musl target and point `CC_<target>` and `AR_<target>` at `musl-gcc` and `ar`, so crates that compile C build for musl as well.

### sccache

The images leave `RUSTC_WRAPPER` unset, because the sccache disk cache only helps when CI keeps its directory between jobs. Opt in per project:

```yaml
variables:
    RUSTC_WRAPPER: sccache
    SCCACHE_DIR: $CI_PROJECT_DIR/.sccache

cache:
    paths:
        - .sccache/
```

## Build

```sh
go run ./scripts build 1.98
```

This builds the base images inline, smoke tests every variant of a line for the local architecture and loads the images. The smoke test builds [`smoke/`](smoke), a crate depending on `zstd` and `ring`, as a static musl binary, rebuilds it to check for an sccache hit and runs its tests with `cargo nextest`. Pass `--platform linux/amd64` to run the same on Apple Silicon through emulation.

`configuration/bases.json` pins the alpine and Debian images by digest, `configuration/lines.json` pins the Rust version of each line, and `configuration/tools.json` pins the tool versions and their sha256 per architecture. `go run ./scripts update` refreshes all three.

GitHub Actions splits the work into four jobs:

- `plan` compares fingerprints with the pushed images and skips bases and lines that did not change.
- `base` builds the four shared bases once on native amd64 and arm64 runners and pushes them to `ghcr.io/panascais-docker/ci-rust/base`. These are internal and not meant to be pulled.
- `build` installs the toolchain of each line on top, again natively per architecture, and pushes by digest.
- `publish` tags both architectures together, so every tag is a manifest list for both platforms.

A monthly run on the 1st rebuilds everything to pick up fresh Alpine and Debian packages, which are the only unpinned inputs.

## Contributors

- Silas Rech [(silas@panascais.net)](mailto:silas@panascais.net)
- Maximilian Schagginger [(max@panascais.net)](mailto:max@panascais.net)

## Contributing

Interested in contributing to **CI-Rust**? Contributions are welcome, and are accepted via pull requests. Please [review these guidelines](contributing.md) before submitting any pull requests.

## License

Code licensed under [MIT](license), documentation under [CC BY 3.0](https://creativecommons.org/licenses/by/3.0/). Rust itself is licensed under [MIT](https://github.com/rust-lang/rust/blob/master/LICENSE-MIT) and [Apache 2.0](https://github.com/rust-lang/rust/blob/master/LICENSE-APACHE).
