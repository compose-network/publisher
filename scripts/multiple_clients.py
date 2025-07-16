#!/usr/bin/env python3
"""
Simulate multiple sequencers connecting to the publisher
"""
import socket
import struct
import threading
import time
import random
from datetime import datetime

class SequencerClient:
    def __init__(self, client_id, chain_id, host='localhost', port=8080):
        self.client_id = client_id
        self.chain_id = chain_id
        self.host = host
        self.port = port
        self.socket = None
        self.running = True

    def connect(self):
        """Connect to publisher"""
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.socket.connect((self.host, self.port))
        print(f"[{self.client_id}] Connected to {self.host}:{self.port}")

    def create_message(self, tx_data):
        """Create protobuf message"""
        # TransactionRequest
        tx_request = b''
        tx_request += b'\x0a' + bytes([len(self.chain_id)]) + self.chain_id
        tx_request += b'\x12' + bytes([len(tx_data)]) + tx_data

        # XTRequest
        xt_request = b''
        xt_request += b'\x0a' + bytes([len(tx_request)]) + tx_request

        # Message
        message = b''
        sender_bytes = self.client_id.encode('utf-8')
        message += b'\x0a' + bytes([len(sender_bytes)]) + sender_bytes
        message += b'\x12' + bytes([len(xt_request)]) + xt_request

        return message

    def send_transaction(self):
        """Send a transaction"""
        tx_data = f"TX from {self.client_id} at {time.time():.2f}".encode()
        message = self.create_message(tx_data)

        # Send with length prefix
        length_prefix = struct.pack('>I', len(message))
        self.socket.sendall(length_prefix + message)
        print(f"[{self.client_id}] Sent transaction ({len(message)} bytes)")

    def receive_broadcasts(self):
        """Receive broadcasts in background"""
        self.socket.settimeout(1.0)
        while self.running:
            try:
                # Read length
                length_data = self.socket.recv(4)
                if not length_data:
                    break

                length = struct.unpack('>I', length_data)[0]

                # Read message
                message_data = b''
                while len(message_data) < length:
                    chunk = self.socket.recv(length - len(message_data))
                    if not chunk:
                        break
                    message_data += chunk

                print(f"[{self.client_id}] Received broadcast ({length} bytes)")

            except socket.timeout:
                continue
            except Exception as e:
                print(f"[{self.client_id}] Receive error: {e}")
                break

    def run(self):
        """Run the client"""
        try:
            self.connect()

            # Start a receiver thread
            receiver = threading.Thread(target=self.receive_broadcasts)
            receiver.start()

            # Send transactions periodically
            for i in range(5):  # Send 5 transactions
                time.sleep(random.uniform(1, 3))  # Random delay
                self.send_transaction()

            # Keep running for a bit to receive broadcasts
            time.sleep(5)

        finally:
            self.running = False
            if self.socket:
                self.socket.close()
            print(f"[{self.client_id}] Disconnected")

def main():
    clients = [
        SequencerClient("sequencer-A", b'\x12\x34'),
        SequencerClient("sequencer-B", b'\x56\x78'),
        SequencerClient("sequencer-C", b'\xAB\xCD'),
    ]

    threads = []
    for client in clients:
        thread = threading.Thread(target=client.run)
        thread.start()
        threads.append(thread)
        time.sleep(0.5)  # Stagger connections

    # Wait for all to finish
    for thread in threads:
        thread.join()

    print("All clients finished")

if __name__ == '__main__':
    main()
