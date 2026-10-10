# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY *.go ./
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/ota-server . \
	&& mkdir -p /out/data/files

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ota-server /ota-server
# Owned by the nonroot user so a fresh named volume is writable for uploads.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/ota-server", "-addr", ":8080", "-manifest", "/data/releases.json", "-files", "/data/files"]
