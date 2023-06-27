FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-pagerduty"]
COPY baton-pagerduty /