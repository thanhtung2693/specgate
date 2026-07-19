import createClient from "openapi-fetch"

import type { paths } from "@/api/schema"

async function registryFetch(request: Request): Promise<Response> {
  return globalThis.fetch(request.url, {
    body: request.body,
    headers: request.headers,
    method: request.method,
    redirect: request.redirect,
    signal: request.signal,
  })
}

export function createRegistryClient(baseUrl: string) {
  return createClient<paths>({
    baseUrl: baseUrl.replace(/\/$/, ""),
    fetch: registryFetch,
  })
}
