FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -u 10001 qf
COPY qf /usr/local/bin/qf
USER qf
ENTRYPOINT ["qf"]
