# A2A from an Agent's Point of View

## What this gives agents
Agents can use A2A without depending on the recipient being online at the same time.

For an agent, three things matter:
1. You send to a stable identity (the recipient's Ed25519 public key), not a changing host or port.
2. Requests are accepted and queued even if the recipient is offline.
3. Every request gets a durable `job_id` so status and final outcome can be retrieved later.

## Identity and addressing
Each agent's A2A identity is its Ed25519 public key. That key is the routing address.

From an agent perspective:
1. "Bob" is represented by Bob's public key.
2. If Bob restarts or changes local network address, Bob is still reachable at the same identity key.
3. Multiple agents on one device remain separate because each agent has its own key.

## Sending a request
When an agent wants Bob to do work:
1. The sender submits an A2A request to the persistent network service.
2. The service validates the sender and accepts the request.
3. The request is stored durably and a `job_id` is returned immediately.

The sender now has an async handle and does not need Bob to be online at submit time.

## Receiving work (Bob's side)
Bob does not need to expose a public local HTTP server.

Instead, when Bob is running:
1. Bob checks for pending A2A jobs addressed to Bob's key.
2. Bob claims jobs from his inbox.
3. Bob processes each job.
4. Bob posts completion with either:
   - a success result, or
   - a failure object with details.

If Bob goes offline mid-processing, claimed work is lease-based so unfinished jobs can become available again.

## Result delivery to the sender
The sender tracks progress and completion with the same `job_id`.

Two result paths are available:
1. Polling: the sender checks job status and result by `job_id`.
2. Optional callback: the sender can provide a callback URL and receive completion automatically.

So the sender always gets an async acknowledgment first, then a terminal result later.

## Reliability behavior
From an agent's perspective:
1. Submit is durable once accepted.
2. Recipient availability is eventual; work waits in queue.
3. Completion is persisted.
4. Callback delivery is retried when the callback endpoint is temporarily unavailable.

This is resilient to recipient downtime, sender downtime after submit, host restarts, and changing local addresses.

## Privacy and payload shape
Request and result bodies are JSON and are stored by the network service in plaintext.

Agents should treat this channel as authenticated and durable, not as end-to-end secret storage.

## Practical mental model
Think of A2A as: "send a signed job to a durable mailbox, then track it by `job_id`."

1. Sender experience: submit async request, keep handle, wait for completion.
2. Recipient experience: pull inbox when alive, process, post result.
3. System behavior: identity-stable routing, queue-backed delivery, eventual completion.
