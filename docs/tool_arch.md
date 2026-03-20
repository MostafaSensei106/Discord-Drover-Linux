# System Architecture

This diagram illustrates how the tool intercepts and processes Discord traffic using Network Namespaces, iptables, and a custom Go-based DPI obfuscator.

## Traffic Flow Diagram

```mermaid
graph TD
    subgraph "Isolated Network Namespace"
        A[fa:fa-discord Discord Process]
    end

    subgraph "Traffic Interception"
        B{iptables}
        C[SOCKS5/HTTP Proxy]
        D[Go DPI Obfuscator]
    end

    subgraph "Remote"
        E((fa:fa-cloud Discord Cloud))
    end

    %% Connections
    A -- "TCP (API/Gateway)" --> B
    A -- "UDP (Voice/Video)" --> B
    
    B -- "REDIRECT" --> C
    B -- "NFQUEUE #1" --> D
    
    C --> E
    D <-->|Modified Packets| E

    style A fill:#5865F2,color:#fff,stroke:#333,stroke-width:2px
    style D fill:#00ADD8,color:#fff,stroke:#333,stroke-width:2px
    style E fill:#f9f,stroke:#333,stroke-dasharray: 5 5
```

## Components Description

1.  **Network Namespace**: Isolates the Discord process to ensure all traffic is captured.
2.  **iptables REDIRECT**: Routes TCP traffic (API/Gateway) through a local SOCKS5/HTTP proxy.
3.  **iptables NFQUEUE**: Sends UDP traffic (Voice/Video) to the Go program for real-time DPI manipulation.
4.  **Go Program**: Performs packet fragmentation or obfuscation to bypass network restrictions.