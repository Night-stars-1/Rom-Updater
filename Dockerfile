# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/ota-server . \
	&& mkdir -p /out/data/files

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ota-server /ota-server
# Owned by the nonroot user so a fresh named volume is writable for uploads.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/ota-server", "-addr", ":8080", "-manifest", "/data/releases.json", "-files", "/data/files"]
