# quote-service

A Flask service with two listeners:

- 8000: the HTTP API (`/quote`)
- 9090: Prometheus metrics

It is stateless and calls no other service. Run with `python app.py`.
