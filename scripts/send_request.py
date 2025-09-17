#!/usr/bin/env python3
"""
Send a cross-chain transaction request to POC Shared Publisher
"""
import socket
import struct
import time
import sys
from datetime import datetime

def create_xt_request_message(sender_id="python-sequencer", chain_id=b'\x12\x34', transactions=None):
    """
    This is the structure:
    Message {
        string sender_id = 1;
        oneof payload {
            XTRequest xt_request = 2;
        }
    }
    XTRequest {
        repeated TransactionRequest transactions = 1;
    }
    TransactionRequest {
        bytes chain_id = 1;
        repeated bytes transaction = 2;
    }
    """
    if transactions is None:
        transactions = [
            b'\x01\x02\x03\x04\x05',  # Sample transaction 1
            b'\x06\x07\x08\x09\x0a',  # Sample transaction 2
        ]


    # Build TransactionRequest
    tx_request = b''
    # Field 1: chain_id (bytes)
    tx_request += b'\x0a'  # Field 1, wire type 2 (length-delimited)
    tx_request += bytes([len(chain_id)])
    tx_request += chain_id

    # Field 2: transactions (repeated bytes)
    for tx in transactions:
        tx_request += b'\x12'  # Field 2, wire type 2
        tx_request += bytes([len(tx)])
        tx_request += tx

    # Build XTRequest
    xt_request = b''
    # Field 1: transactions (repeated TransactionRequest)
    xt_request += b'\x0a'  # Field 1, wire type 2
    xt_request += bytes([len(tx_request)])
    xt_request += tx_request

    # Build Message
    message = b''
    # Field 1: sender_id (string)
    message += b'\x0a'  # Field 1, wire type 2
    message += bytes([len(sender_id)])
    message += sender_id.encode('utf-8')

    # Field 2: xt_request (oneof payload)
    message += b'\x12'  # Field 2, wire type 2
    message += bytes([len(xt_request)])
    message += xt_request

    return message

def send_request(host='localhost', port=8080):
    """Send a request to the publisher"""
    try:
        # Connect
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Connecting to {host}:{port}...")
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.connect((host, port))
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Connected!")

        # Create message
        message = create_xt_request_message(
            sender_id=f"python-sequencer-{int(time.time())}",
            chain_id=b'\x12\x34',
            transactions=[
                b'Transaction data 1 at ' + str(time.time()).encode(),
                b'Transaction data 2 at ' + str(time.time()).encode(),
            ]
        )

        # Send with length prefix (4 bytes, big-endian)
        length_prefix = struct.pack('>I', len(message))
        data = length_prefix + message

        print(f"[{datetime.now().strftime('%H:%M:%S')}] Sending message ({len(message)} bytes)...")
        sock.sendall(data)
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Message sent successfully!")

        # Keep connection open to receive broadcasts
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Waiting for broadcasts...")
        sock.settimeout(5.0)  # 5 second timeout

        try:
            while True:
                # Read length prefix
                length_data = sock.recv(4)
                if not length_data:
                    break

                length = struct.unpack('>I', length_data)[0]
                print(f"[{datetime.now().strftime('%H:%M:%S')}] Receiving broadcast ({length} bytes)...")

                # Read message
                message_data = b''
                while len(message_data) < length:
                    chunk = sock.recv(length - len(message_data))
                    if not chunk:
                        break
                    message_data += chunk

                print(f"[{datetime.now().strftime('%H:%M:%S')}] Received broadcast message")

        except socket.timeout:
            print(f"[{datetime.now().strftime('%H:%M:%S')}] No more broadcasts (timeout)")

        sock.close()
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Connection closed")

    except Exception as e:
        print(f"[{datetime.now().strftime('%H:%M:%S')}] Error: {e}")
        return 1

    return 0

if __name__ == '__main__':
    host = sys.argv[1] if len(sys.argv) > 1 else 'localhost'
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 8080

    exit(send_request(host, port))
