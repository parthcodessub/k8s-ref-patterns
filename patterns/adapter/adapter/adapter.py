import time
from http.server import BaseHTTPRequestHandler, HTTPServer
import os

LOG_FILE = "/var/log/app/status.txt"

class AdapterHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        # We only care about the /metrics endpoint
        if self.path == "/metrics":
            self.send_response(200)
            self.send_header("Content-type", "text/plain")
            self.end_headers()
            
            # 1. READ: Access the shared file
            try:
                if not os.path.exists(LOG_FILE):
                    self.wfile.write(b"# Status file not yet created by legacy app\n")
                    return

                with open(LOG_FILE, "r") as f:
                    lines = f.readlines()
                
                # 2. TRANSFORM: Convert "Legacy Format" -> "Prometheus Format"
                # Legacy: "MemUsageMB: 400"
                # Prom:   "legacy_memory_usage_bytes 419430400"
                
                output = []
                for line in lines:
                    if "MemUsageMB" in line:
                        # Extract the number (e.g., 400)
                        parts = line.split(":")
                        mb_value = int(parts[1].strip())
                        bytes_value = mb_value * 1024 * 1024
                        
                        output.append("# HELP legacy_memory_usage_bytes Memory usage converted to bytes")
                        output.append("# TYPE legacy_memory_usage_bytes gauge")
                        output.append(f"legacy_memory_usage_bytes {bytes_value}")
                    
                    elif "CPU_Load" in line:
                        parts = line.split(":")
                        cpu_value = int(parts[1].strip())
                        
                        output.append("# HELP legacy_cpu_load_percent CPU load percentage")
                        output.append("# TYPE legacy_cpu_load_percent gauge")
                        output.append(f"legacy_cpu_load_percent {cpu_value}")

                # 3. RESPOND
                response_text = "\n".join(output) + "\n"
                self.wfile.write(response_text.encode('utf-8'))

            except Exception as e:
                self.wfile.write(f"# Error parsing log file: {e}\n".encode())
        else:
            self.send_response(404)

if __name__ == "__main__":
    server_address = ('', 8080)
    print("Starting Adapter on :8080...")
    httpd = HTTPServer(server_address, AdapterHandler)
    httpd.serve_forever()