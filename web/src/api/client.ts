// Connect client for the nats-lens API, served by the same binary under /api.
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { LensService } from "../gen/natslens/v1/service_pb";
import { demoFetch } from "./demoFetch";

/**
 * Every call, query or mutation, gives up after 45 seconds with
 * deadline_exceeded: longer than the server's own limits (30 seconds a
 * query, 40 seconds a GetTimeSeriesBatch call), so a slow answer still
 * arrives, but a call that never returns cannot hold a page or a dialog
 * for good. The client sends it as Connect-Timeout-Ms, and the server
 * applies it too, so it bounds every call; a server limit raised above
 * it would never be reached from the web.
 */
export const CALL_TIMEOUT_MS = 45_000;

export const lens = createClient(LensService, createConnectTransport({ baseUrl: "/api", defaultTimeoutMs: CALL_TIMEOUT_MS, ...(demoFetch ? { fetch: demoFetch } : {}) }));
