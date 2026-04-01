FROM golang:1.23 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/athena ./cmd/athena

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/athena /usr/local/bin/athena

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/athena"]
