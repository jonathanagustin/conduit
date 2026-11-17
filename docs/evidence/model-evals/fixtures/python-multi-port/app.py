"""quote-service: an HTTP API on 8000 and Prometheus metrics on 9090."""
from flask import Flask, jsonify
from prometheus_client import Counter, start_http_server

app = Flask(__name__)
served = Counter("quotes_served_total", "Quotes served")


@app.get("/quote")
def quote():
    served.inc()
    return jsonify(text="Measure twice, cut once.")


if __name__ == "__main__":
    start_http_server(9090)
    app.run(host="0.0.0.0", port=8000)
