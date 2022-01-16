FROM golang:alpine3.11 AS builder

WORKDIR /go-build
RUN mkdir ./goperf
COPY ./main.go ./go.mod ./go.sum ./
COPY ./lib ./lib
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -a -installsuffix cgo -o GoPerf main.go

FROM alpine:3.11
COPY --from=builder /go-build/GoPerf /
RUN chmod +x /GoPerf
CMD ["/GoPerf", "-server", "-port", "8080"]
