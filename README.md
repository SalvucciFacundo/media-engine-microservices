# Media Engine Microservices

An asynchronous, event-driven media and document processing system built with Go, NATS, PostgreSQL, HTMX, Templ, and Tailwind CSS.

## 🏛️ Architecture Overview

The system is designed to showcase clean, decoupled microservices with real-time UI feedback:

```
[ Browser: HTMX + Tailwind + SSE ]
              │  ▲
         HTTP │  │ Server-Sent Events (SSE)
              ▼  │
┌─────────────────────────────────┐
│       Web Gateway (UI)          │  (Go + Templ + HTMX)
└──────────────┬──────────────────┘
               │ Emits task / Listens to events
               ▼
┌─────────────────────────────────┐
│        NATS Message Bus         │  (Lightweight Pub/Sub & Work Queues)
└──────┬───────────────────▲──────┘
       │ Work Dispatch     │ Progress Updates
       ▼                   │
┌──────────────────────────┴──────┐       ┌───────────────────────────────┐
│          Worker Engine          │ ◄───► │      PostgreSQL Storage       │
│ (Image & PDF Processing + TTL)  │       │     (Metadata & Lifecycles)   │
└─────────────────────────────────┘       └───────────────────────────────┘
```

## 🚀 Key Features & Architectural Patterns

- **Asynchronous Task Processing:** HTTP uploads return `202 Accepted` immediately without blocking user requests.
- **Real-Time Reactive UI:** Live DOM updates and progress tracking using HTMX and Server-Sent Events (SSE).
- **Decoupled Event-Driven Pipeline:** Services communicate through NATS topics and worker queues.
- **Ephemeral Storage & TTL Janitor:** Automatic cleanup of processed files to prevent disk exhaustion.
- **Polymorphic File Handlers:**
  - **Images (JPEG/PNG):** EXIF metadata extraction, resizing, and WebP optimization.
  - **PDF Documents:** Metadata inspection, page count extraction, and word estimation.
- **Clean / Hexagonal Architecture:** Clear domain, port, and adapter separation inside a structured Go monorepo.

## 🛠️ Tech Stack

- **Backend:** Go (Golang)
- **Frontend / SSR:** Templ (Type-safe Go HTML templates) + HTMX + Tailwind CSS
- **Message Broker:** NATS
- **Database:** PostgreSQL (logical isolation per service/domain)
- **Deployment:** Docker & Docker Compose
