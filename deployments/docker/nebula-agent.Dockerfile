FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/nebula-agent ./cmd/nebula-agent

FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/nebula-agent /usr/local/bin/nebula-agent
COPY deployments/certs/nebula-agent.crt deployments/certs/nebula-agent.key /etc/nebula/tls/
COPY deployments/certs/nebula-api-client.crt /etc/nebula/tls/
EXPOSE 7071
ENTRYPOINT ["/usr/local/bin/nebula-agent"]
