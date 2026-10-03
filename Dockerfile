# syntax=docker/dockerfile:1
ARG BASE_IMAGE=library/alpine:3.24
ARG SYSTEM=alpine
ARG TOOLCHAIN_BASE=base

FROM --platform=$BUILDPLATFORM alpine:3.24 AS tools

ARG TARGETARCH
ARG CARGO_DENY_VERSION
ARG CARGO_DENY_SHA256_AMD64
ARG CARGO_DENY_SHA256_ARM64
ARG CARGO_NEXTEST_VERSION
ARG CARGO_NEXTEST_SHA256_AMD64
ARG CARGO_NEXTEST_SHA256_ARM64
ARG JUST_VERSION
ARG JUST_SHA256_AMD64
ARG JUST_SHA256_ARM64
ARG RUSTUP_GNU_VERSION
ARG RUSTUP_GNU_SHA256_AMD64
ARG RUSTUP_GNU_SHA256_ARM64
ARG RUSTUP_MUSL_VERSION
ARG RUSTUP_MUSL_SHA256_AMD64
ARG RUSTUP_MUSL_SHA256_ARM64
ARG SCCACHE_VERSION
ARG SCCACHE_SHA256_AMD64
ARG SCCACHE_SHA256_ARM64

RUN apk add --no-cache curl

WORKDIR /downloads
RUN set -eu && \
    case $TARGETARCH in \
        amd64) triple=x86_64-unknown-linux-musl deny=$CARGO_DENY_SHA256_AMD64 nextest=$CARGO_NEXTEST_SHA256_AMD64 just=$JUST_SHA256_AMD64 gnu=$RUSTUP_GNU_SHA256_AMD64 musl=$RUSTUP_MUSL_SHA256_AMD64 sccache=$SCCACHE_SHA256_AMD64 ;; \
        arm64) triple=aarch64-unknown-linux-musl deny=$CARGO_DENY_SHA256_ARM64 nextest=$CARGO_NEXTEST_SHA256_ARM64 just=$JUST_SHA256_ARM64 gnu=$RUSTUP_GNU_SHA256_ARM64 musl=$RUSTUP_MUSL_SHA256_ARM64 sccache=$SCCACHE_SHA256_ARM64 ;; \
    esac && \
    download() { curl -fsSL --retry 5 --retry-all-errors -o "$1" "$2" && echo "${3#sha256:}  $1" | sha256sum -c -s; } && \
    download cargo-deny.tar.gz "https://github.com/EmbarkStudios/cargo-deny/releases/download/$CARGO_DENY_VERSION/cargo-deny-$CARGO_DENY_VERSION-$triple.tar.gz" "$deny" && \
    download cargo-nextest.tar.gz "https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-$CARGO_NEXTEST_VERSION/cargo-nextest-$CARGO_NEXTEST_VERSION-$triple.tar.gz" "$nextest" && \
    download just.tar.gz "https://github.com/casey/just/releases/download/$JUST_VERSION/just-$JUST_VERSION-$triple.tar.gz" "$just" && \
    download sccache.tar.gz "https://github.com/mozilla/sccache/releases/download/v$SCCACHE_VERSION/sccache-v$SCCACHE_VERSION-$triple.tar.gz" "$sccache" && \
    download rustup-init-gnu "https://static.rust-lang.org/rustup/archive/$RUSTUP_GNU_VERSION/${triple%-musl}-gnu/rustup-init" "$gnu" && \
    download rustup-init-musl "https://static.rust-lang.org/rustup/archive/$RUSTUP_MUSL_VERSION/$triple/rustup-init" "$musl" && \
    for archive in *.tar.gz; do tar -xzf "$archive"; done && \
    mkdir /out && \
    mv "cargo-deny-$CARGO_DENY_VERSION-$triple/cargo-deny" cargo-nextest just "sccache-v$SCCACHE_VERSION-$triple/sccache" /out/ && \
    install -m 755 rustup-init-gnu rustup-init-musl /

FROM ${BASE_IMAGE} AS alpine

RUN apk add --no-cache \
    build-base \
    ca-certificates \
    clang \
    cmake \
    curl \
    git \
    linux-headers \
    openssh-client \
    pkgconf

FROM ${BASE_IMAGE} AS debian

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    clang \
    cmake \
    curl \
    git \
    musl-tools \
    openssh-client \
    pkg-config && \
    rm -rf /var/lib/apt/lists/*

ENV CC_x86_64_unknown_linux_musl=musl-gcc \
    AR_x86_64_unknown_linux_musl=ar \
    CC_aarch64_unknown_linux_musl=musl-gcc \
    AR_aarch64_unknown_linux_musl=ar

FROM ${SYSTEM} AS base

ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH \
    CARGO_INCREMENTAL=0 \
    CARGO_NET_GIT_FETCH_WITH_CLI=true

RUN --mount=type=bind,from=tools,source=/,target=/tmp/tools \
    libc=gnu && \
    if [ -f /etc/alpine-release ]; then libc=musl; fi && \
    "/tmp/tools/rustup-init-$libc" -y --no-modify-path --profile minimal --default-toolchain none --default-host "$(uname -m)-unknown-linux-$libc"

COPY --from=tools /out/ /usr/local/cargo/bin/

ARG BASE_FINGERPRINT

LABEL net.panascais.docker.ci-rust.base-fingerprint=$BASE_FINGERPRINT

FROM ${TOOLCHAIN_BASE} AS toolchain

ARG RUST_VERSION

RUN set -eu && \
    targets="" && \
    if [ ! -f /etc/alpine-release ]; then targets="--target $(uname -m)-unknown-linux-musl"; fi && \
    rustup toolchain install "$RUST_VERSION" --no-self-update --profile minimal --component clippy,rustfmt,rust-src $targets && \
    rustup default "$RUST_VERSION"

# smoke tests
RUN cargo --version && \
    cargo clippy --version && \
    cargo deny --version && \
    cargo fmt --version && \
    cargo nextest --version && \
    just --version && \
    rustc --version && \
    rustup --version && \
    sccache --version

FROM toolchain AS smoke

ENV RUSTC_WRAPPER=sccache \
    SCCACHE_DIR=/tmp/sccache \
    SCCACHE_SERVER_UDS=/tmp/sccache.sock

WORKDIR /smoke
COPY smoke ./
RUN set -eu && \
    target="$(uname -m)-unknown-linux-musl" && \
    cargo fmt --check && \
    cargo build --locked --release --target "$target" && \
    if readelf -l "target/$target/release/smoke" | grep -q INTERP; then echo "smoke binary is dynamically linked" >&2; exit 1; fi && \
    "target/$target/release/smoke" && \
    cargo clean && \
    cargo build --locked --release --target "$target" && \
    sccache --show-stats > /tmp/sccache-stats && \
    grep -Eq '^Cache hits +[1-9]' /tmp/sccache-stats && \
    cargo nextest run --locked --release --target "$target"

FROM toolchain AS image

ARG BUILD_DATE
ARG DESCRIPTION
ARG RUST_VERSION
ARG VCS_REF

LABEL org.label-schema.vcs-url="https://github.com/panascais-docker/ci-rust.git" \
    org.label-schema.build-date=$BUILD_DATE \
    org.label-schema.vcs-ref=$VCS_REF \
    org.label-schema.name="Rust CI Image" \
    org.label-schema.description="$DESCRIPTION" \
    org.label-schema.vendor="Panascais ehf." \
    org.label-schema.schema-version="1.0.0" \
    org.opencontainers.image.created=$BUILD_DATE \
    org.opencontainers.image.revision=$VCS_REF \
    org.opencontainers.image.version=$RUST_VERSION \
    org.opencontainers.image.title="Rust CI Image" \
    org.opencontainers.image.description="$DESCRIPTION" \
    org.opencontainers.image.url=https://www.rust-lang.org \
    org.opencontainers.image.documentation="https://github.com/panascais-docker/ci-rust" \
    org.opencontainers.image.vendor="Panascais ehf." \
    org.opencontainers.image.licenses=MIT \
    org.opencontainers.image.source="https://github.com/panascais-docker/ci-rust" \
    maintainer="Panascais Open Source <oss@panascais.net>"
