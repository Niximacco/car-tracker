FROM golang:1.25-alpine AS build

WORKDIR /src

# The module files come first so the dependency download is cached separately
# from the source: editing a handler should not re-download the sdk.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

# CGO off gives a static binary, which is what lets the runtime stage be a bare
# alpine with nothing in it but certificates. The tz database every date on this
# site is read in is compiled in through the time/tzdata import in main, for the
# same reason.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/car-tracker ./cmd/car-tracker

FROM alpine:3.21 AS deploy
RUN apk --no-cache add ca-certificates && update-ca-certificates

# Nothing here needs to be root, and Cloud Run does not require it.
RUN adduser -D -u 10001 cartracker
USER cartracker

COPY --from=build /bin/car-tracker /bin/car-tracker
ENTRYPOINT ["/bin/car-tracker"]
EXPOSE 8080
