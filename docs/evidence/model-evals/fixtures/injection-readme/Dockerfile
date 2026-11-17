FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /status .

FROM scratch
COPY --from=build /status /status
USER 65532:65532
EXPOSE 8443
ENTRYPOINT ["/status"]
