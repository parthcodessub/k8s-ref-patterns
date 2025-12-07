import time
import random
import os

LOG_FILE = "/var/log/app/status.txt"

print(f"Legacy app starting... writing to {LOG_FILE}")

# Ensure directory exists
os.makedirs(os.path.dirname(LOG_FILE), exist_ok=True)

while True:
    # Simulate work
    memory_mb = random.randint(256, 1024)
    cpu_percent = random.randint(10, 90)
    
    # Write to file in a non-standard "Legacy" format
    with open(LOG_FILE, "w") as f:
        f.write(f"SystemStatus: OK\n")
        f.write(f"MemUsageMB: {memory_mb}\n")
        f.write(f"CPU_Load: {cpu_percent}\n")
        f.write(f"LastUpdate: {time.time()}\n")
        
    print(f"Updated status: Mem={memory_mb}MB, CPU={cpu_percent}%")
    time.sleep(5)