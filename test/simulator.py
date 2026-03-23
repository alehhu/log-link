import time
import uuid
import random
import threading
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

# Metric State
current_load = 10.0

class MetricHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/load':
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(f"{current_load:.2f}".encode())
    def log_message(self, format, *args):
        return # Silent

def run_metric_server():
    try:
        server = HTTPServer(('localhost', 8080), MetricHandler)
        server.serve_forever()
    except Exception:
        pass

def get_ts():
    return time.strftime('%Y-%m-%dT%H:%M:%S')

def generate_logs():
    global current_load
    
    files = {
        "api": open("api.log", "w"),
        "worker": open("worker.log", "w"),
        "db": open("db.log", "w")
    }
    
    print("🚀 Simulator running...")
    print("  - API Logs:    api.log")
    print("  - Worker Logs: worker.log")
    print("  - DB Logs:     db.log")
    print("  - Metrics:     http://localhost:8080/load")
    print("\nPress Ctrl+C to stop.\n")
    
    endpoints = ["/v1/user", "/v1/auth", "/v1/payment", "/v1/search", "/v1/health"]
    methods = ["GET", "POST", "PUT", "DELETE"]
    
    try:
        while True:
            # Update load (sine wave with some noise)
            current_load = 40 + 30 * (time.time() % 120 / 60) + random.uniform(-5, 5)
            
            request_id = str(uuid.uuid4())
            user_id = f"user-{random.randint(100, 999)}"
            ip = f"192.168.1.{random.randint(2, 254)}"
            
            # 1. API receives request
            method = random.choice(methods)
            endpoint = random.choice(endpoints)
            files["api"].write(f"{get_ts()} [INFO] {method} {endpoint} - remote_addr={ip} request_id={request_id} user_id={user_id}\n")
            files["api"].flush()
            
            # Simulated processing time
            time.sleep(random.uniform(0.05, 0.2))
            
            # 2. DB operation
            if random.random() > 0.05:
                files["db"].write(f"{get_ts()} [DEBUG] query=\"SELECT * FROM accounts WHERE id='{user_id}'\" duration=12ms request_id={request_id}\n")
            else:
                # Occasional DB error
                files["db"].write(f"{get_ts()} [ERROR] connection pool exhausted for request_id={request_id}\n")
                files["api"].write(f"{get_ts()} [ERROR] 500 Internal Server Error - {method} {endpoint} request_id={request_id}\n")
            files["db"].flush()
            
            # 3. Background Task
            if random.random() > 0.7:
                task_id = f"task-{random.randint(1000, 9999)}"
                files["worker"].write(f"{get_ts()} [INFO] starting background job task_id={task_id} trigger=request request_id={request_id}\n")
                files["worker"].flush()
                time.sleep(0.1)
                files["worker"].write(f"{get_ts()} [INFO] background job task_id={task_id} completed successfully\n")
                files["worker"].flush()
            
            # 4. Noisy logs (background chatter)
            if random.random() > 0.8:
                files["api"].write(f"{get_ts()} [DEBUG] cache hit for key=session:{user_id}\n")
                files["api"].flush()

            # Randomly simulate a timeout or panic
            if random.random() > 0.98:
                files["api"].write(f"{get_ts()} [FATAL] unexpected panic in middleware: request_id={request_id}\n")
                files["api"].flush()
            
            time.sleep(random.uniform(0.2, 0.8))
            
    except KeyboardInterrupt:
        for f in files.values():
            f.close()
        print("\nSimulator stopped.")

if __name__ == "__main__":
    t = threading.Thread(target=run_metric_server, daemon=True)
    t.start()
    generate_logs()
