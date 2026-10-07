FROM golang:1.25.5-alpine@sha256:ac09a5f469f307e5da71e766b0bd59c9c49ea460a528cc3e6686513d64a6f1fb AS builder

RUN apk add --no-cache gcc=15.2.0-r2 musl-dev=1.2.5-r23 tzdata=2026c-r0 \
    && ln -sf /usr/share/zoneinfo/Asia/Ho_Chi_Minh /etc/localtime \
    && echo "Asia/Ho_Chi_Minh" > /etc/timezone

WORKDIR /app

COPY commonkit/go.mod commonkit/go.sum ./commonkit/
COPY src/go.mod src/go.sum ./src/

WORKDIR /app/src
RUN --mount=type=cache,target=/go/pkg/mod go mod download

WORKDIR /app
COPY commonkit ./commonkit
COPY src ./src

ARG BRANCH=unknown
ARG COMMIT=unknown
ARG TIME=unknown

WORKDIR /app/src
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 go build \
    -ldflags "-X 'doorservice/version.Branch=${BRANCH}' -X 'doorservice/version.Commit=${COMMIT}' -X 'doorservice/version.Time=${TIME}'" \
    -o /app/doorservice .

FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

RUN apk add --no-cache tzdata=2026c-r0 \
    && ln -sf /usr/share/zoneinfo/Asia/Ho_Chi_Minh /etc/localtime \
    && echo "Asia/Ho_Chi_Minh" > /etc/timezone

WORKDIR /app
COPY --from=builder /app/doorservice /app/doorservice

CMD ["/app/doorservice"]
