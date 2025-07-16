FROM golang:1.24

WORKDIR /app

RUN go install github.com/air-verse/air@latest

COPY . .

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o /gulabo-bot server.go

EXPOSE 80
EXPOSE 8080

ENTRYPOINT ["air"]

