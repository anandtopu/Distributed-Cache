FROM golang:1.22 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/cache-node ./cmd/cache-node

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /out/cache-node /cache-node

USER nonroot:nonroot
EXPOSE 8080 9090

ENTRYPOINT ["/cache-node"]
