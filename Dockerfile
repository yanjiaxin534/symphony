FROM golang:1.24.0-alpine

RUN if echo "${BUILD_BASE_IMAGE}" | grep "mariner"; then \
        export SKIP_RACE_CHECK=true; \
        CGO_ENABLED=1 mage generate operatorTest; \
    else \
        CGO_ENABLED=1 mage generate operatorTest; \
    fi