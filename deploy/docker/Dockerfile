FROM scratch

COPY bin/keywatcher /keywatcher
COPY internal/server/static /internal/server/static

WORKDIR /
EXPOSE 8080
ENTRYPOINT ["/keywatcher"]
