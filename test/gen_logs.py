import time
import uuid
import random

def generate_logs():
    app_log = open("app.log", "w")
    db_log = open("db.log", "w")
    
    print("Generating logs... Press Ctrl+C to stop.")
    
    try:
        while True:
            request_id = str(uuid.uuid4())
            
            # Step 1: App receives request
            app_log.write(f"{time.strftime('%H:%M:%S')} [INFO] Received request {request_id}\n")
            app_log.flush()
            time.sleep(random.uniform(0.1, 0.5))
            
            # Step 2: DB query
            db_log.write(f"{time.strftime('%H:%M:%S')} [DEBUG] Executing query for {request_id}\n")
            db_log.flush()
            time.sleep(random.uniform(0.1, 0.5))
            
            # Step 3: App finishes
            app_log.write(f"{time.strftime('%H:%M:%S')} [INFO] Request {request_id} completed\n")
            app_log.flush()
            time.sleep(random.uniform(0.2, 1.0))
            
    except KeyboardInterrupt:
        app_log.close()
        db_log.close()
        print("\nStopped.")

if __name__ == "__main__":
    generate_logs()
