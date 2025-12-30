FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY gobore .
EXPOSE 7835
ENTRYPOINT ["./gobore"]
CMD ["server", "--port", "7835"]
