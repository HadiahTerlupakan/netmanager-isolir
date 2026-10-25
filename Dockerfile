FROM golang:1.26-alpine3.22 AS build
WORKDIR /src
# Tanpa go.sum: modul ini hanya memakai pustaka standar.
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/netmanager-isolir ./cmd/netmanager-isolir

FROM alpine:3.22
COPY --from=build /out/netmanager-isolir /usr/local/bin/netmanager-isolir
# 65532 = "nonroot" pada image distroless. Port 8080/8443/8081 di atas 1024,
# jadi tidak butuh capability.
USER 65532:65532
EXPOSE 8080 8443 8081
ENTRYPOINT ["/usr/local/bin/netmanager-isolir"]
