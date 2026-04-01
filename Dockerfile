FROM --platform=$BUILDPLATFORM golang:1.23 AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/athena ./cmd/athena

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/athena /usr/local/bin/athena

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/athena"]
