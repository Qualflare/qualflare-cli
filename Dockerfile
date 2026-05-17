FROM alpine:3.23
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -u 10001 qf
COPY qf /usr/local/bin/qf
USER qf
ENTRYPOINT ["qf"]
