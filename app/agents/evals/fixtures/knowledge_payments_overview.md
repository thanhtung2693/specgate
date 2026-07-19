# Payments domain overview

The payments service settles supplier invoices through the gateway adapter
layer. Each adapter is responsible for currency conversion and PCI-scoped
tokenization. The platform supports three rails: card, bank transfer, and
wallet.

Retry policy: failed authorizations retry up to three times with exponential
backoff (1s, 4s, 16s). Permanent failures surface a structured
`payment_error_code` for downstream consumers.

Idempotency keys are required on every charge request. Duplicate keys return
the original result without re-charging.
