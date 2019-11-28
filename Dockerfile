FROM ubuntu:18.04

RUN apt-get update \
    && apt-get install -y \ 
    gcc \
    git \
    wget \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN wget -P /tmp https://dl.google.com/go/go1.13.4.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf /tmp/go1.13.4.linux-amd64.tar.gz \
    && rm -rf /tmp/*

ENV GOPATH /working/go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH
RUN mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH"

# Add Maintainer Info
LABEL maintainer="Nic Grobler <nic.grobler2011@gmail.com>"

# Set the Current Working Directory inside the container
WORKDIR /working

# Copy files into container
COPY . .

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Build the Go app as static (needed for sqlite cgo)
RUN go build -a -installsuffix cgo -ldflags "-w -s" -o /sql3net *.go \
   && rm -rf /working

WORKDIR /
# Command to run the executable
CMD ["/sql3net"]
