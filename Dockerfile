# Etapa 1: Build da aplicação
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .

# Baixar dependências e compilar
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o webhook-server

# Etapa 2: Imagem final
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/webhook-server .

# Expor a porta e rodar a aplicação
EXPOSE 443
CMD ["./webhook-server"]