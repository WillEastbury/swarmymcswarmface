FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/scaler ./main.go

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/scaler /scaler
EXPOSE 8080 9090
ENTRYPOINT ["/scaler"]
