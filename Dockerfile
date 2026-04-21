FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY qf /usr/local/bin/qf
ENTRYPOINT ["qf"]
