FROM ubuntu:latest
LABEL authors="EDY"

ENTRYPOINT ["top", "-b"]