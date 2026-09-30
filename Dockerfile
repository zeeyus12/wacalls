FROM node:22-alpine AS client
WORKDIR /app/client
COPY client/package*.json ./
RUN npm ci
COPY client/ ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /wacalls ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=build /wacalls /app/wacalls
COPY --from=client /app/client/dist /app/client/dist
ENV PORT=8080
CMD ["sh","-c","/app/wacalls -addr 0.0.0.0:${PORT} -db /app/data/wacalls.db -static /app/client/dist -max-calls-per-session 1"]
