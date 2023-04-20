FROM gcr.io/distroless/static-debian11:nonroot
ENTRYPOINT ["/baton-pager-duty"]
COPY baton-pager-duty /