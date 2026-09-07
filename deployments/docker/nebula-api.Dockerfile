FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/nebula-api ./cmd/nebula-api

FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/nebula-api /usr/local/bin/nebula-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/nebula-api"]
