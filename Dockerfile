FROM        golang:latest
MAINTAINER  Samuel BERTHE <samuel.berthe@iadvize.com>

RUN go get github.com/samber/prometheus-inception-exporter

EXPOSE     9000
ENTRYPOINT [ "/go/bin/prometheus-inception-exporter" ]
