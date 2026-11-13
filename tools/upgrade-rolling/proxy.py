"""Loopback TCP passthrough: Hub mTLS remains end-to-end during replacement."""
from collections import Counter
import select
import socket
import socketserver
import threading


class Passthrough:
    def __init__(self, backends):
        self.lock = threading.Lock()
        self.next = 0
        self.connections = Counter()
        self.active = set()
        self.stopping = threading.Event()
        self.set_backends(backends)
        owner = self

        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                with owner.lock:
                    name, address = owner.backends[owner.next % len(owner.backends)]
                    owner.next += 1
                try:
                    with socket.create_connection(address, timeout=5) as upstream:
                        pair = (self.request, upstream)
                        with owner.lock:
                            owner.connections[name] += 1
                            owner.active.add(pair)
                        try:
                            while not owner.stopping.is_set():
                                ready, _, _ = select.select(pair, [], [], 0.2)
                                for source in ready:
                                    data = source.recv(65536)
                                    if not data:
                                        return
                                    destination = upstream if source is self.request else self.request
                                    destination.sendall(data)
                        finally:
                            with owner.lock:
                                owner.active.discard(pair)
                except OSError:
                    # A stopped backend closes its TCP connections; the actual
                    # Agent transport, not this fixture, owns request retries.
                    return

        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True
            allow_reuse_address = True

        self.server = Server(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": 0.1}, daemon=True)
        self.thread.start()

    @property
    def address(self):
        return self.server.server_address

    def set_backends(self, backends):
        backends = list(backends)
        if not backends or len({name for name, _ in backends}) != len(backends):
            raise ValueError("unique nonempty backend set required")
        for name, (host, port) in backends:
            if not name or host != "127.0.0.1" or not isinstance(port, int) or not 0 < port < 65536:
                raise ValueError("fixture backends must be named loopback listeners")
        with self.lock:
            self.backends = backends
            self.next = 0

    def counts(self):
        with self.lock:
            return dict(self.connections)

    def close(self):
        self.stopping.set()
        self.server.shutdown()
        self.server.server_close()
        with self.lock:
            active = list(self.active)
        for pair in active:
            for connection in pair:
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
        self.thread.join(timeout=3)
        if self.thread.is_alive():
            raise RuntimeError("fixture proxy did not stop")
