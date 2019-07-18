FROM quay.io/operator-framework/upstream-registry-builder:v1.1.0 as builder

FROM scratch
COPY --from=builder /build/bin/configmap-server /bin/configmap-server
COPY --from=builder /bin/grpc_health_probe /bin/grpc_health_probe
EXPOSE 50051
ENTRYPOINT ["/bin/configmap-server"]
