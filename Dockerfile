FROM --platform=linux/amd64 alpine:3.20
COPY shdgjy-api-amd64 /shdgjy-api
RUN chmod +x /shdgjy-api && mkdir /data
EXPOSE 8443
ENV SHDGJY_DATA=/data/shdgjy-data.json SHDGJY_ADDR=:8443
ENTRYPOINT ["/shdgjy-api"]
