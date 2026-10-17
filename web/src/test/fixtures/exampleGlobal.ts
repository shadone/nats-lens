// Responses of the demo system example-global (natslens demo), as JSON.
// Regenerate from a running demo when its scenario changes.

import type { JsonValue } from "@bufbuild/protobuf";

export const exampleGlobalServers: JsonValue = {
  "servers": [
    {
      "name": "nats-eu-0",
      "serverId": "NeuFAKE0GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:33:49Z",
      "uptime": "548396s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "37580963840",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "104857600",
      "cpuPercent": 3,
      "status": "STATUS_OK",
      "streamLeaders": 2,
      "consumerLeaders": 66,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1a",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-1",
      "serverId": "NeuFAKE1GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:34:49Z",
      "uptime": "548336s",
      "role": "SERVER_ROLE_META_LEADER",
      "present": true,
      "healthz": "ok",
      "connections": "64",
      "jsStorageBytes": "39728447488",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "112197632",
      "cpuPercent": 4,
      "status": "STATUS_OK",
      "streamLeaders": 1,
      "consumerLeaders": 2,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1b",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-2",
      "serverId": "NeuFAKE2GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:35:49Z",
      "uptime": "548276s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "38654705664",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "119537664",
      "cpuPercent": 5,
      "status": "STATUS_OK",
      "streamLeaders": 1,
      "consumerLeaders": 1,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1c",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-3",
      "serverId": "NeuFAKE3GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:36:49Z",
      "uptime": "548216s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "44023414784",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "126877696",
      "cpuPercent": 22,
      "status": "STATUS_OK",
      "streamLeaders": 1,
      "consumerLeaders": 3,
      "slowConsumersInRange": "1",
      "tags": [
        "az:eu-1a",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-4",
      "serverId": "NeuFAKE4GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:37:49Z",
      "uptime": "548156s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "35433480192",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "134217728",
      "cpuPercent": 3,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1b",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-5",
      "serverId": "NeuFAKE5GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:38:49Z",
      "uptime": "548096s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "38654705664",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "141557760",
      "cpuPercent": 6,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1c",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-6",
      "serverId": "NeuFAKE6GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:39:49Z",
      "uptime": "548036s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "40802189312",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "148897792",
      "cpuPercent": 4,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1a",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-7",
      "serverId": "NeuFAKE7GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:40:49Z",
      "uptime": "547976s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "31138512895",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "156237824",
      "cpuPercent": 2,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1b",
        "region:eu"
      ]
    },
    {
      "name": "nats-eu-8",
      "serverId": "NeuFAKE8GLOBAL",
      "version": "2.12.4",
      "cluster": "eu",
      "startTime": "2026-10-11T09:41:49Z",
      "uptime": "547916s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "61",
      "jsStorageBytes": "41875931136",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "163577856",
      "cpuPercent": 5,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:eu-1c",
        "region:eu"
      ]
    },
    {
      "name": "nats-us-0",
      "serverId": "NusFAKE0GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:33:49Z",
      "uptime": "548396s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "52",
      "jsStorageBytes": "33285996544",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "104857600",
      "cpuPercent": 6,
      "status": "STATUS_OK",
      "streamLeaders": 2,
      "consumerLeaders": 24,
      "slowConsumersInRange": "1",
      "tags": [
        "az:us-1a",
        "region:us"
      ]
    },
    {
      "name": "nats-us-1",
      "serverId": "NusFAKE1GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-17T12:09:59Z",
      "uptime": "20626s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "restarts": [
        "2026-10-17T12:09:59Z"
      ],
      "connections": "50",
      "jsStorageBytes": "32212254720",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "112197632",
      "cpuPercent": 4,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1b",
        "region:us"
      ]
    },
    {
      "name": "nats-us-2",
      "serverId": "NusFAKE2GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-17T12:22:19Z",
      "uptime": "19886s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "restarts": [
        "2026-10-17T12:22:19Z"
      ],
      "connections": "50",
      "jsStorageBytes": "35433480192",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "119537664",
      "cpuPercent": 8,
      "status": "STATUS_OK",
      "streamLeaders": 1,
      "consumerLeaders": 1,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1c",
        "region:us"
      ]
    },
    {
      "name": "nats-us-3",
      "serverId": "NusFAKE3GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-17T12:30:29Z",
      "uptime": "19396s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "restarts": [
        "2026-10-17T12:30:29Z"
      ],
      "connections": "50",
      "jsStorageBytes": "34359738368",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "126877696",
      "cpuPercent": 34,
      "status": "STATUS_WARNING",
      "streamLeaders": 1,
      "consumerLeaders": 2,
      "slowConsumersInRange": "11",
      "tags": [
        "az:us-1a",
        "region:us"
      ]
    },
    {
      "name": "nats-us-4",
      "serverId": "NusFAKE4GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:37:49Z",
      "uptime": "529085s",
      "role": "SERVER_ROLE_FOLLOWER",
      "connections": "49",
      "jsStorageBytes": "30064771072",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "134217728",
      "cpuPercent": 5,
      "status": "STATUS_WARNING",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1b",
        "region:us"
      ]
    },
    {
      "name": "nats-us-5",
      "serverId": "NusFAKE5GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:38:49Z",
      "uptime": "548096s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "50",
      "jsStorageBytes": "33285996544",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "141557760",
      "cpuPercent": 5,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1c",
        "region:us"
      ]
    },
    {
      "name": "nats-us-6",
      "serverId": "NusFAKE6GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:39:49Z",
      "uptime": "548036s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "50",
      "jsStorageBytes": "28991029248",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "148897792",
      "cpuPercent": 3,
      "status": "STATUS_OK",
      "streamLeaders": 1,
      "consumerLeaders": 2,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1a",
        "region:us"
      ]
    },
    {
      "name": "nats-us-7",
      "serverId": "NusFAKE7GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:40:49Z",
      "uptime": "547976s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "50",
      "jsStorageBytes": "36507222016",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "156237824",
      "cpuPercent": 7,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1b",
        "region:us"
      ]
    },
    {
      "name": "nats-us-8",
      "serverId": "NusFAKE8GLOBAL",
      "version": "2.12.4",
      "cluster": "us",
      "startTime": "2026-10-11T09:41:49Z",
      "uptime": "547916s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "50",
      "jsStorageBytes": "31138512895",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "163577856",
      "cpuPercent": 4,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:us-1c",
        "region:us"
      ]
    },
    {
      "name": "nats-ap-0",
      "serverId": "NapFAKE0GLOBAL",
      "version": "2.12.4",
      "cluster": "ap",
      "startTime": "2026-10-11T09:33:49Z",
      "uptime": "548396s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "33",
      "jsStorageBytes": "51539607552",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "104857600",
      "cpuPercent": 9,
      "status": "STATUS_OK",
      "streamLeaders": 2,
      "consumerLeaders": 1,
      "slowConsumersInRange": "0",
      "tags": [
        "az:ap-1a",
        "region:ap"
      ]
    },
    {
      "name": "nats-ap-1",
      "serverId": "NapFAKE1GLOBAL",
      "version": "2.12.4",
      "cluster": "ap",
      "startTime": "2026-10-11T09:34:49Z",
      "uptime": "548336s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "31",
      "jsStorageBytes": "55834574848",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "112197632",
      "cpuPercent": 12,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:ap-1b",
        "region:ap"
      ]
    },
    {
      "name": "nats-ap-2",
      "serverId": "NapFAKE2GLOBAL",
      "version": "2.12.4",
      "cluster": "ap",
      "startTime": "2026-10-11T09:35:49Z",
      "uptime": "548276s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "31",
      "jsStorageBytes": "76235669504",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "119537664",
      "cpuPercent": 71,
      "status": "STATUS_INFO",
      "streamLeaders": 2,
      "consumerLeaders": 3,
      "slowConsumersInRange": "1",
      "tags": [
        "az:ap-1c",
        "region:ap"
      ]
    },
    {
      "name": "nats-ap-3",
      "serverId": "NapFAKE3GLOBAL",
      "version": "2.12.4",
      "cluster": "ap",
      "startTime": "2026-10-11T09:36:49Z",
      "uptime": "548216s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "31",
      "jsStorageBytes": "53687091200",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "126877696",
      "cpuPercent": 8,
      "status": "STATUS_OK",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:ap-1a",
        "region:ap"
      ]
    },
    {
      "name": "nats-ap-4",
      "serverId": "NapFAKE4GLOBAL",
      "version": "2.12.2",
      "cluster": "ap",
      "startTime": "2026-10-11T09:37:49Z",
      "uptime": "548156s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "31",
      "jsStorageBytes": "56908316672",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "134217728",
      "cpuPercent": 11,
      "status": "STATUS_MUTED",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:ap-1b",
        "region:ap"
      ]
    },
    {
      "name": "nats-ap-5",
      "serverId": "NapFAKE5GLOBAL",
      "version": "2.12.2",
      "cluster": "ap",
      "startTime": "2026-10-11T09:38:49Z",
      "uptime": "548096s",
      "role": "SERVER_ROLE_FOLLOWER",
      "present": true,
      "healthz": "ok",
      "connections": "31",
      "jsStorageBytes": "50465865728",
      "jsMaxStorageBytes": "107374182400",
      "memBytes": "141557760",
      "cpuPercent": 7,
      "status": "STATUS_MUTED",
      "streamLeaders": 0,
      "consumerLeaders": 0,
      "slowConsumersInRange": "0",
      "tags": [
        "az:ap-1c",
        "region:ap"
      ]
    }
  ],
  "expected": [
    "nats-eu-0",
    "nats-eu-1",
    "nats-eu-2",
    "nats-eu-3",
    "nats-eu-4",
    "nats-eu-5",
    "nats-eu-6",
    "nats-eu-7",
    "nats-eu-8",
    "nats-us-0",
    "nats-us-1",
    "nats-us-2",
    "nats-us-3",
    "nats-us-4",
    "nats-us-5",
    "nats-us-6",
    "nats-us-7",
    "nats-us-8",
    "nats-ap-0",
    "nats-ap-1",
    "nats-ap-2",
    "nats-ap-3",
    "nats-ap-4",
    "nats-ap-5"
  ],
  "missing": [
    "nats-us-4"
  ]
};

export const exampleGlobalAp3: JsonValue = {
  "server": {
    "name": "nats-ap-3",
    "serverId": "NapFAKE3GLOBAL",
    "version": "2.12.4",
    "cluster": "ap",
    "startTime": "2026-10-11T09:36:49Z",
    "uptime": "548216s",
    "role": "SERVER_ROLE_FOLLOWER",
    "present": true,
    "healthz": "ok",
    "connections": "31",
    "jsStorageBytes": "53687091200",
    "jsMaxStorageBytes": "107374182400",
    "memBytes": "126877696",
    "cpuPercent": 8,
    "status": "STATUS_OK",
    "streamLeaders": 0,
    "consumerLeaders": 0,
    "slowConsumersInRange": "0",
    "tags": [
      "az:ap-1a",
      "region:ap"
    ]
  },
  "endpoints": [
    {
      "endpoint": "STATSZ",
      "state": "ok"
    },
    {
      "endpoint": "VARZ",
      "state": "ok"
    },
    {
      "endpoint": "CONNZ",
      "state": "ok"
    },
    {
      "endpoint": "ROUTEZ",
      "state": "ok"
    },
    {
      "endpoint": "GATEWAYZ",
      "state": "ok"
    },
    {
      "endpoint": "LEAFZ",
      "state": "ok"
    },
    {
      "endpoint": "ACCOUNTZ",
      "state": "ok"
    },
    {
      "endpoint": "ACCSTATZ",
      "state": "ok"
    },
    {
      "endpoint": "JSZ",
      "state": "ok"
    },
    {
      "endpoint": "HEALTHZ",
      "state": "ok"
    },
    {
      "endpoint": "RAFTZ",
      "state": "ok"
    },
    {
      "endpoint": "RAFTZ:APP",
      "state": "ok"
    },
    {
      "endpoint": "IPQUEUESZ",
      "state": "ok"
    }
  ],
  "processes": [
    {
      "serverId": "NapFAKE3OLDGLOBAL",
      "startTime": "2026-10-02T11:35:49Z",
      "firstSeen": "2026-10-03T03:23:49Z",
      "lastSeen": "2026-10-11T09:34:49Z"
    },
    {
      "serverId": "NapFAKE3GLOBAL",
      "startTime": "2026-10-11T09:36:49Z",
      "firstSeen": "2026-10-11T09:37:19Z",
      "lastSeen": "2026-10-17T17:53:45Z"
    }
  ],
  "subscriptions": "1840",
  "slowConsumers": "0",
  "routes": 5,
  "jsStreams": 2,
  "jsConsumers": 2,
  "metaLeader": "nats-eu-1",
  "configDigest": "sha256:7f3a91c2",
  "routeViews": [
    {
      "remoteName": "nats-ap-0",
      "rtt": "0.000317s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-ap-1",
      "rtt": "0.000354s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-ap-2",
      "rtt": "0.000391s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-ap-4",
      "rtt": "0.000428s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-ap-5",
      "rtt": "0.000465s",
      "pendingBytes": "0"
    }
  ],
  "gateways": 2,
  "leafnodes": 0,
  "limits": {
    "maxPayload": "8388608",
    "maxPending": "67108864",
    "writeDeadline": "10s",
    "pingInterval": "120s",
    "maxPingsOut": 2,
    "jsMaxStorage": "107374182400",
    "jsMaxMemory": "2147483648",
    "maxConnections": 65536
  },
  "limitsDiffer": [
    "max_payload"
  ],
  "kubernetes": {
    "pod": "nats-ap-3",
    "namespace": "nats",
    "node": "gke-example-pool-ap-3f89",
    "scheduledAt": "2026-10-11T09:36:09Z"
  },
  "configLoadedAt": "2026-10-11T09:36:49Z"
};

export const exampleGlobalUs4: JsonValue = {
  "server": {
    "name": "nats-us-4",
    "serverId": "NusFAKE4GLOBAL",
    "version": "2.12.4",
    "cluster": "us",
    "startTime": "2026-10-11T09:37:49Z",
    "uptime": "529085s",
    "role": "SERVER_ROLE_FOLLOWER",
    "connections": "49",
    "jsStorageBytes": "30064771072",
    "jsMaxStorageBytes": "107374182400",
    "memBytes": "134217728",
    "cpuPercent": 5,
    "status": "STATUS_WARNING",
    "streamLeaders": 0,
    "consumerLeaders": 0,
    "slowConsumersInRange": "0",
    "tags": [
      "az:us-1b",
      "region:us"
    ]
  },
  "endpoints": [
    {
      "endpoint": "STATSZ",
      "state": "missing"
    },
    {
      "endpoint": "VARZ",
      "state": "missing"
    },
    {
      "endpoint": "CONNZ",
      "state": "missing"
    },
    {
      "endpoint": "ROUTEZ",
      "state": "missing"
    },
    {
      "endpoint": "GATEWAYZ",
      "state": "missing"
    },
    {
      "endpoint": "LEAFZ",
      "state": "missing"
    },
    {
      "endpoint": "ACCOUNTZ",
      "state": "missing"
    },
    {
      "endpoint": "ACCSTATZ",
      "state": "missing"
    },
    {
      "endpoint": "JSZ",
      "state": "missing"
    },
    {
      "endpoint": "HEALTHZ",
      "state": "missing"
    },
    {
      "endpoint": "RAFTZ",
      "state": "missing"
    },
    {
      "endpoint": "RAFTZ:APP",
      "state": "missing"
    },
    {
      "endpoint": "IPQUEUESZ",
      "state": "missing"
    }
  ],
  "processes": [
    {
      "serverId": "NusFAKE4OLDGLOBAL",
      "startTime": "2026-10-02T11:35:49Z",
      "firstSeen": "2026-10-03T03:23:49Z",
      "lastSeen": "2026-10-11T09:35:49Z"
    },
    {
      "serverId": "NusFAKE4GLOBAL",
      "startTime": "2026-10-11T09:37:49Z",
      "firstSeen": "2026-10-11T09:38:19Z",
      "lastSeen": "2026-10-17T12:35:54Z"
    }
  ],
  "subscriptions": "1840",
  "slowConsumers": "0",
  "routes": 8,
  "jsStreams": 2,
  "jsConsumers": 3,
  "metaLeader": "nats-eu-1",
  "configDigest": "sha256:7f3a91c2",
  "routeViews": [
    {
      "remoteName": "nats-us-0",
      "rtt": "0.000317s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-1",
      "rtt": "0.000354s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-2",
      "rtt": "0.000391s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-3",
      "rtt": "0.000428s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-5",
      "rtt": "0.000465s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-6",
      "rtt": "0.000502s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-7",
      "rtt": "0.000539s",
      "pendingBytes": "0"
    },
    {
      "remoteName": "nats-us-8",
      "rtt": "0.000576s",
      "pendingBytes": "0"
    }
  ],
  "gateways": 2,
  "leafnodes": 0,
  "limits": {
    "maxPayload": "1048576",
    "maxPending": "67108864",
    "writeDeadline": "10s",
    "pingInterval": "120s",
    "maxPingsOut": 2,
    "jsMaxStorage": "107374182400",
    "jsMaxMemory": "2147483648",
    "maxConnections": 65536
  },
  "kubernetes": {
    "pod": "nats-us-4",
    "namespace": "nats"
  },
  "configLoadedAt": "2026-10-11T09:37:49Z"
};

export const exampleGlobalCpu: JsonValue = {
  "series": {
    "metric": "cluster.cpu_percent",
    "entity": {
      "type": "ENTITY_TYPE_SYSTEM",
      "name": "eu"
    },
    "unit": "UNIT_PERCENT",
    "step": "14400s",
    "points": [
      {
        "time": "2026-10-16T16:00:00Z",
        "value": 5.595226086956549,
        "low": 1.0322,
        "high": 81.06212000000001
      },
      {
        "time": "2026-10-16T20:00:00Z",
        "value": 5.66090499999999,
        "low": 1.0322,
        "high": 79.91192
      },
      {
        "time": "2026-10-17T00:00:00Z",
        "value": 5.71533749999999,
        "low": 1.1928,
        "high": 80.65103
      },
      {
        "time": "2026-10-17T04:00:00Z",
        "value": 5.765435624999997,
        "low": 1.0577999999999999,
        "high": 81.46043
      },
      {
        "time": "2026-10-17T08:00:00Z",
        "value": 5.6418234375,
        "low": 1.18,
        "high": 80.51471
      },
      {
        "time": "2026-10-17T12:00:00Z",
        "value": 5.695099375000041,
        "low": 1.0568,
        "high": 81.34967
      },
      {
        "time": "2026-10-17T16:00:00Z",
        "value": 5.880221052631576,
        "low": 1.0706,
        "high": 81.59675
      }
    ],
    "min": 1.0322,
    "max": 81.59675,
    "top": {
      "type": "ENTITY_TYPE_SERVER",
      "key": "nats-ap-2",
      "name": "nats-ap-2",
      "server": "nats-ap-2"
    },
    "topPoints": [
      {
        "time": "2026-10-16T16:00:00Z",
        "value": 72.4906043083003
      },
      {
        "time": "2026-10-16T20:00:00Z",
        "value": 71.06736124999979
      },
      {
        "time": "2026-10-17T00:00:00Z",
        "value": 70.14587000000006
      },
      {
        "time": "2026-10-17T04:00:00Z",
        "value": 70.50408274999972
      },
      {
        "time": "2026-10-17T08:00:00Z",
        "value": 68.58756199999983
      },
      {
        "time": "2026-10-17T12:00:00Z",
        "value": 72.0139332500002
      },
      {
        "time": "2026-10-17T16:00:00Z",
        "value": 73.01341052631582
      }
    ]
  }
};
