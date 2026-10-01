# Changelog

## 0.3.0
- `VerifyWebhook(rawBody, signature, secret, tolerance)` checks a webhook delivery's `Depllo-Signature` and returns a `*WebhookEvent`; `EventPipelineFinished`, `EventJobFinished`, `EventWebhookEndpointDisabled`.
- `Client.API` gains the webhook routes: `WebhookEndpointsCreate`, `WebhookEndpointsList`, `WebhookEndpointsGet`, `WebhookEndpointsUpdate`, `WebhookEndpointsDelete`, `WebhookEndpointsEventTypes`, `WebhookDeliveriesList`, `WebhookDeliveriesGet`, `WebhookDeliveriesRetry`.

## 0.2.0
- A route read by id next to its list is named `get` + the list's name: `client.API.ProjectsGetPipelines` (was `client.API.ProjectsPipelines2`). Each old name stays as a deprecated alias.

