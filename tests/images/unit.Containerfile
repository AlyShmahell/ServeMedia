FROM docker.io/library/golang:1.26-bookworm
WORKDIR /src
ENV PATH="/usr/local/go/bin:${PATH}"
COPY src/ /src/
WORKDIR /src
RUN go mod download
CMD ["go", "test", "./..."]
