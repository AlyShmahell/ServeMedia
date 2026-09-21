FROM docker.io/library/python:3.12-alpine
COPY omdb.py webhook.py /stub/
EXPOSE 8080 9090
