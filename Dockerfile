# Build a static binary, then ship it on a distroless base that runs as a non-root user.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/kaasu ./cmd/kaasu
RUN mkdir /data-empty

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kaasu /usr/local/bin/kaasu
ENV KAASU_DATABASE_PATH=/data/kaasu.db
# Created with the runtime user as owner before VOLUME, so a fresh volume is writable by uid 65532.
COPY --from=build --chown=65532:65532 /data-empty /data
VOLUME ["/data"]
EXPOSE 8080 8443
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s CMD ["/usr/local/bin/kaasu", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/kaasu"]
CMD ["serve"]
