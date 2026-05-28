FROM node:24-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY backend/go.mod backend/go.mod
COPY backend backend
WORKDIR /src/backend
RUN go test ./... && CGO_ENABLED=0 go build -o /out/minihub ./cmd/minihub && CGO_ENABLED=0 go build -o /out/minihub-ssh ./cmd/minihub-ssh

FROM alpine:3.22
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY --from=build /out/minihub /usr/local/bin/minihub
COPY --from=build /out/minihub-ssh /usr/local/bin/minihub-ssh
COPY --from=frontend-build /src/frontend/dist /app/frontend
ENV MINIHUB_ADDR=:8080
ENV MINIHUB_DATA=/data
ENV MINIHUB_FRONTEND=/app/frontend
EXPOSE 8080
EXPOSE 2222
VOLUME ["/data"]
CMD ["minihub"]
