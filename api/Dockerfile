##
## Copyright (c) Microsoft Corporation.
## Licensed under the MIT license.
## SPDX-License-Identifier: MIT
##

ARG BUILD_BASE_IMAGE=ubuntu:22.04
ARG TARGET_BASE_IMAGE=ubuntu:latest

FROM --platform=$BUILDPLATFORM ${BUILD_BASE_IMAGE} AS build


FROM ${TARGET_BASE_IMAGE}

ARG TARGETARCH

RUN \
    set -x \
    && apt-get update \
    && apt-get install -y gcc g++ openssl libc6 libc6-dev libssl-dev ca-certificates curl wget jq android-tools-adb binutils \
    && if [ "${TARGETARCH}" = "arm64" ]; then \
        wget -O helm-v3.16.2-linux-arm64.tar.gz https://get.helm.sh/helm-v3.16.2-linux-arm64.tar.gz && \
        tar -zxvf helm-v3.16.2-linux-arm64.tar.gz && \
        mv linux-arm64/helm /usr/local/bin/helm && \
        chmod +x /usr/local/bin/helm; \
    else \
        curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 && \
        chmod +x get_helm.sh && \
        ./get_helm.sh; \
    fi \
    && apt upgrade -y
ADD https://github.com/golang/go/raw/master/lib/time/zoneinfo.zip /zoneinfo.zip
ENV ZONEINFO=/zoneinfo.zip
COPY --from=build /dist /
COPY --from=build /extensions /extensions
ADD ./api/symphony-api.json /
ADD ./api/symphony-api-no-k8s.json /
EXPOSE 8080
EXPOSE 8081
ENV LOG_LEVEL=Debug
ENV GOFAIL_HTTP="127.0.0.1:22381"
# ENV CONFIG /symphony-api.json
CMD sh -c 'if [ -f /etc/pki/ca-trust/source/anchors/proxy-cert.crt ]; then update-ca-trust; fi && exec /symphony-api -c $CONFIG -l $LOG_LEVEL'