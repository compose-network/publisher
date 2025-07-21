#!/usr/bin/env python3
"""
Phase 2 Test Script - Full 2PC Protocol Testing
Simulates sequencers that participate in the complete Two-Phase Commit protocol
"""
import socket
import struct
import threading
import time
import random
import argparse
from datetime import datetime
from enum import Enum

class MessageType(Enum):
    XT_REQUEST = 2
    VOTE = 3
    DECIDED = 4
    BLOCK = 5

class SequencerState(Enum):
    IDLE = "idle"
    VOTING = "voting"
    COMMITTED = "committed"
    ABORTED = "aborted"

class Phase2Sequencer:
    def __init__(self, client_id, chain_id, host='localhost', port=8080, vote_strategy='commit'):
        self.client_id = client_id
        self.chain_id = chain_id
        self.host = host
        self.port = port
        self.socket = None
        self.running = True
        self.vote_strategy = vote_strategy  # 'commit', 'abort', 'random', 'delay'
        self.state = SequencerState.IDLE
        self.active_transactions = {}  # xt_id -> transaction_data
        self.vote_delay = random.uniform(0.5, 2.0)  # Random vote delay

    def connect(self):
        """Connect to publisher"""
        self.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.socket.connect((self.host, self.port))
        print(f"[{self.client_id}] Connected to {self.host}:{self.port}")

    def create_xt_request_message(self, participating_chains=None):
        """Create XTRequest message with transactions for multiple chains"""
        if participating_chains is None:
            # Create a cross-chain transaction involving multiple chains
            participating_chains = [
                bytes([0x12, 0x34]),  # Chain A
                bytes([0x13, 0x35]),  # Chain B
                bytes([0x14, 0x36]),  # Chain C
            ]

        # Build XTRequest with multiple TransactionRequests
        xt_request = b''

        for i, chain_id in enumerate(participating_chains):
            # Create short transaction data (similar to original)
            tx_data = bytes([0x01, 0x02, 0x03, 0x04, 0x05 + i])  # Simple test data

            # Build TransactionRequest
            tx_request = b''
            # Field 1: chain_id (bytes)
            tx_request += b'\x0a'  # Field 1, wire type 2
            tx_request += bytes([len(chain_id)])
            tx_request += chain_id

            # Field 2: transactions (repeated bytes) - add one transaction
            tx_request += b'\x12'  # Field 2, wire type 2
            tx_request += bytes([len(tx_data)])
            tx_request += tx_data

            # Add this TransactionRequest to XTRequest
            # Field 1: transactions (repeated TransactionRequest)
            xt_request += b'\x0a'  # Field 1, wire type 2
            tx_request_len = len(tx_request)
            if tx_request_len > 127:
                # Use proper varint encoding for lengths > 127
                xt_request += self.encode_varint(tx_request_len)
            else:
                xt_request += bytes([tx_request_len])
            xt_request += tx_request

        # Build Message
        message = b''
        # Field 1: sender_id (string)
        message += b'\x0a'  # Field 1, wire type 2
        message += bytes([len(self.client_id)])
        message += self.client_id.encode('utf-8')

        # Field 2: xt_request (oneof payload)
        message += b'\x12'  # Field 2, wire type 2
        xt_request_len = len(xt_request)
        if xt_request_len > 127:
            # Use proper varint encoding for lengths > 127
            message += self.encode_varint(xt_request_len)
        else:
            message += bytes([xt_request_len])
        message += xt_request

        return message

    def create_vote_message(self, xt_id, vote_decision):
        """Create Vote message"""
        # Build Vote
        vote = b''
        # Field 1: sender_chain_id (bytes)
        vote += b'\x0a'  # Field 1, wire type 2
        vote += bytes([len(self.chain_id)])
        vote += self.chain_id

        # Field 2: xt_id (uint32)
        vote += b'\x10'  # Field 2, wire type 0 (varint)
        vote += self.encode_varint(xt_id)

        # Field 3: vote (bool)
        vote += b'\x18'  # Field 3, wire type 0 (varint)
        vote += b'\x01' if vote_decision else b'\x00'

        # Build Message
        message = b''
        # Field 1: sender_id (string)
        message += b'\x0a'  # Field 1, wire type 2
        message += bytes([len(self.client_id)])
        message += self.client_id.encode('utf-8')

        # Field 3: vote (oneof payload)
        message += b'\x1a'  # Field 3, wire type 2
        message += bytes([len(vote)])
        message += vote

        return message

    def create_block_message(self, included_xt_ids):
        """Create Block message"""
        # Build Block
        block = b''
        # Field 1: chain_id (bytes)
        block += b'\x0a'  # Field 1, wire type 2
        block += bytes([len(self.chain_id)])
        block += self.chain_id

        # Field 2: block_data (bytes)
        block_data = f"Block from {self.client_id} at {time.time():.2f} with {len(included_xt_ids)} TXs".encode()
        block += b'\x12'  # Field 2, wire type 2
        block += bytes([len(block_data)])
        block += block_data

        # Field 3: included_xt_ids (repeated uint32)
        for xt_id in included_xt_ids:
            block += b'\x18'  # Field 3, wire type 0 (varint)
            block += self.encode_varint(xt_id)

        # Build Message
        message = b''
        # Field 1: sender_id (string)
        message += b'\x0a'  # Field 1, wire type 2
        message += bytes([len(self.client_id)])
        message += self.client_id.encode('utf-8')

        # Field 5: block (oneof payload)
        message += b'\x2a'  # Field 5, wire type 2
        message += bytes([len(block)])
        message += block

        return message

    def encode_varint(self, value):
        """Encode uint32 as varint"""
        result = b''
        while value >= 0x80:
            result += bytes([value & 0x7f | 0x80])
            value >>= 7
        result += bytes([value & 0x7f])
        return result

    def decode_varint(self, data, offset=0):
        """Decode varint from data starting at offset"""
        result = 0
        shift = 0
        pos = offset
        while pos < len(data):
            byte = data[pos]
            result |= (byte & 0x7f) << shift
            pos += 1
            if (byte & 0x80) == 0:
                break
            shift += 7
        return result, pos

    def parse_message(self, data):
        """Parse incoming message"""
        try:
            # Very basic protobuf parsing - just enough for our test
            pos = 0
            sender_id = ""
            message_type = None
            xt_id = None
            decision = None

            while pos < len(data):
                if pos >= len(data):
                    break

                # Read field header
                field_header = data[pos]
                pos += 1
                field_number = field_header >> 3
                wire_type = field_header & 0x07

                if field_number == 1 and wire_type == 2:  # sender_id
                    length = data[pos]
                    pos += 1
                    sender_id = data[pos:pos+length].decode('utf-8')
                    pos += length

                elif field_number == 2 and wire_type == 2:  # xt_request
                    message_type = MessageType.XT_REQUEST
                    length = data[pos]
                    pos += 1
                    # Parse xt_id from the nested message if needed
                    pos += length

                elif field_number == 4 and wire_type == 2:  # decided
                    message_type = MessageType.DECIDED
                    length = data[pos]
                    pos += 1
                    decided_data = data[pos:pos+length]
                    pos += length

                    # Parse decided message
                    d_pos = 0
                    while d_pos < len(decided_data):
                        d_field_header = decided_data[d_pos]
                        d_pos += 1
                        d_field_number = d_field_header >> 3
                        d_wire_type = d_field_header & 0x07

                        if d_field_number == 1 and d_wire_type == 0:  # xt_id
                            xt_id, d_pos = self.decode_varint(decided_data, d_pos)
                        elif d_field_number == 2 and d_wire_type == 0:  # decision
                            decision_val, d_pos = self.decode_varint(decided_data, d_pos)
                            decision = bool(decision_val)
                        else:
                            d_pos += 1
                else:
                    # Skip unknown fields
                    if wire_type == 2:  # length-delimited
                        if pos < len(data):
                            length = data[pos]
                            pos += 1 + length
                    else:
                        pos += 1

            return {
                'sender_id': sender_id,
                'message_type': message_type,
                'xt_id': xt_id,
                'decision': decision
            }

        except Exception as e:
            print(f"[{self.client_id}] Error parsing message: {e}")
            return None

    def decide_vote(self, xt_id):
        """Decide how to vote based on strategy"""
        if self.vote_strategy == 'commit':
            return True
        elif self.vote_strategy == 'abort':
            return False
        elif self.vote_strategy == 'random':
            return random.choice([True, False])
        elif self.vote_strategy == 'delay':
            # Delay vote to test timeout scenarios
            time.sleep(random.uniform(3, 6))  # Longer than typical timeout
            return True
        else:
            return True  # Default to commit

    def send_transaction(self):
        """Send a transaction to initiate 2PC"""
        message = self.create_xt_request_message()
        length_prefix = struct.pack('>I', len(message))
        self.socket.sendall(length_prefix + message)
        print(f"[{self.client_id}] Sent XTRequest ({len(message)} bytes)")

        # Also vote for own transaction after sending
        current_xt_id = getattr(self, '_current_xt_id', 0) + 1
        self._current_xt_id = current_xt_id
        threading.Thread(target=self.send_vote_after_delay, args=(current_xt_id,)).start()

    def handle_xt_request(self, parsed_msg):
        """Handle incoming XTRequest broadcast"""
        print(f"[{self.client_id}] Received XTRequest broadcast from {parsed_msg['sender_id']}")

        # In real implementation, we'd need to parse the xt_id from the XTRequest
        # For now, let's extract it from the message context or use a counter
        # Since we can't easily parse the xt_id from XTRequest, we'll use a simple approach
        # The coordinator assigns sequential IDs starting from 1
        current_xt_id = getattr(self, '_current_xt_id', 0) + 1
        self._current_xt_id = current_xt_id

        # Simulate transaction validation and send vote
        threading.Thread(target=self.send_vote_after_delay, args=(current_xt_id,)).start()

    def send_vote_after_delay(self, xt_id):
        """Send vote after validation delay"""
        time.sleep(self.vote_delay)

        vote_decision = self.decide_vote(xt_id)
        message = self.create_vote_message(xt_id, vote_decision)

        length_prefix = struct.pack('>I', len(message))
        self.socket.sendall(length_prefix + message)

        vote_str = "COMMIT" if vote_decision else "ABORT"
        print(f"[{self.client_id}] Sent Vote: {vote_str} for xt_id={xt_id}")

        self.active_transactions[xt_id] = vote_decision

    def handle_decided(self, parsed_msg):
        """Handle Decided message from SP"""
        xt_id = parsed_msg['xt_id']
        decision = parsed_msg['decision']

        decision_str = "COMMIT" if decision else "ABORT"
        print(f"[{self.client_id}] Received Decided: {decision_str} for xt_id={xt_id}")

        if decision:  # Commit
            self.state = SequencerState.COMMITTED
            # Send block after short delay
            threading.Thread(target=self.send_block_after_delay, args=(xt_id,)).start()
        else:  # Abort
            self.state = SequencerState.ABORTED
            if xt_id in self.active_transactions:
                del self.active_transactions[xt_id]

    def send_block_after_delay(self, xt_id):
        """Send block after including transaction"""
        time.sleep(random.uniform(0.5, 1.5))  # Block creation time

        message = self.create_block_message([xt_id])
        length_prefix = struct.pack('>I', len(message))
        self.socket.sendall(length_prefix + message)

        print(f"[{self.client_id}] Sent Block with xt_id={xt_id}")

        if xt_id in self.active_transactions:
            del self.active_transactions[xt_id]

    def receive_messages(self):
        """Receive and handle messages"""
        self.socket.settimeout(1.0)
        while self.running:
            try:
                # Read length prefix
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

                # Parse and handle message
                parsed_msg = self.parse_message(message_data)
                if parsed_msg:
                    if parsed_msg['message_type'] == MessageType.XT_REQUEST:
                        self.handle_xt_request(parsed_msg)
                    elif parsed_msg['message_type'] == MessageType.DECIDED:
                        self.handle_decided(parsed_msg)

            except socket.timeout:
                continue
            except Exception as e:
                print(f"[{self.client_id}] Receive error: {e}")
                break

    def run(self, send_initial_tx=False, tx_count=1):
        """Run the sequencer"""
        try:
            self.connect()

            # Start receiver thread
            receiver = threading.Thread(target=self.receive_messages)
            receiver.start()

            # Optionally send initial transaction
            if send_initial_tx:
                for i in range(tx_count):
                    time.sleep(random.uniform(1, 3))
                    self.send_transaction()

            # Keep running to participate in 2PC
            while self.running:
                time.sleep(1)

        except KeyboardInterrupt:
            print(f"[{self.client_id}] Interrupted by user")
        finally:
            self.running = False
            if self.socket:
                self.socket.close()
            print(f"[{self.client_id}] Disconnected")

def main():
    parser = argparse.ArgumentParser(description='Phase 2 Test Script - 2PC Protocol Testing')
    parser.add_argument('--host', default='localhost', help='Publisher host')
    parser.add_argument('--port', type=int, default=8080, help='Publisher port')
    parser.add_argument('--clients', type=int, default=3, help='Number of sequencer clients')
    parser.add_argument('--vote-strategy', choices=['commit', 'abort', 'random', 'delay'],
                       default='commit', help='Vote strategy for all clients')
    parser.add_argument('--send-tx', action='store_true', help='Send initial transactions')
    parser.add_argument('--tx-count', type=int, default=1, help='Number of transactions to send')
    parser.add_argument('--duration', type=int, default=30, help='Test duration in seconds')

    args = parser.parse_args()

    print(f"Starting Phase 2 Test with {args.clients} clients")
    print(f"Vote strategy: {args.vote_strategy}")
    print(f"Duration: {args.duration} seconds")
    print("-" * 50)

    # Create sequencer clients
    clients = []
    threads = []

    # Use predefined chain IDs that match the XTRequest
    predefined_chains = [
        bytes([0x12, 0x34]),  # Chain A
        bytes([0x13, 0x35]),  # Chain B
        bytes([0x14, 0x36]),  # Chain C
    ]

    for i in range(args.clients):
        client_id = f"sequencer-{chr(65 + i)}"  # A, B, C, ...
        chain_id = predefined_chains[i] if i < len(predefined_chains) else bytes([0x15 + i, 0x37 + i])

        client = Phase2Sequencer(
            client_id=client_id,
            chain_id=chain_id,
            host=args.host,
            port=args.port,
            vote_strategy=args.vote_strategy
        )

        clients.append(client)

        # Only first client sends initial transaction
        send_tx = args.send_tx and i == 0
        thread = threading.Thread(target=client.run, args=(send_tx, args.tx_count))
        thread.start()
        threads.append(thread)

        time.sleep(0.5)  # Stagger connections

    try:
        # Run for specified duration
        time.sleep(args.duration)

        # Stop all clients
        for client in clients:
            client.running = False

        # Wait for threads to finish
        for thread in threads:
            thread.join(timeout=2)

    except KeyboardInterrupt:
        print("\nStopping all clients...")
        for client in clients:
            client.running = False

    print("\nAll clients finished")

if __name__ == '__main__':
    main()
