# ZooKeeper Component

This component deploys [Apache ZooKeeper](https://zookeeper.apache.org/), a distributed coordination service for maintaining configuration information, naming, providing distributed synchronization, and group services.

## Overview

ZooKeeper provides:
- Distributed configuration management
- Naming and service registry
- Distributed synchronization (locks, barriers)
- Group membership services
- Leader election

## Common Use Cases

ZooKeeper is commonly used as a dependency for:
- **ClickHouse** - Distributed query coordination
- **Apache Kafka** - Broker coordination and metadata
- **Apache Hadoop** - HDFS and YARN coordination
- **Apache HBase** - Region server coordination
- **Custom distributed systems** - Leader election and configuration

## Architecture

| Service | Port | Protocol | Description |
|---------|------|----------|-------------|
| `zookeeper` | 2181 | TCP | Main client connection port |
| `admin` | 8080 | HTTP | Admin server for monitoring |

## System Requirements

- **RAM**: Minimum 512MB, 1GB+ recommended for production
- **Storage**: SSD recommended for transaction logs

## Example Usage

### As a Dependency

```yaml
# In another component's architect.yml
name: my-distributed-app

dependencies:
  zookeeper:
    component: ../zookeeper
    variables:
      max_client_connections: "100"

builds:
  app:
    context: ./app

deployments:
  app:
    image: ${{ builds.app.image }}
    environment:
      ZOOKEEPER_HOST: ${{ dependencies.zookeeper.services.zookeeper.host }}
      ZOOKEEPER_PORT: ${{ dependencies.zookeeper.services.zookeeper.port }}
```

### With ClickHouse

```yaml
dependencies:
  zookeeper:
    component: ../zookeeper

databases:
  clickhouse:
    type: clickhouse:^24

deployments:
  clickhouse-config:
    # ClickHouse uses ZooKeeper for distributed coordination
    environment:
      ZOOKEEPER_HOST: ${{ dependencies.zookeeper.services.zookeeper.host }}
```

## Configuration Variables

### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `tick_time` | `2000` | Basic time unit in milliseconds |
| `init_limit` | `10` | Ticks for followers to connect to leader |
| `sync_limit` | `5` | Ticks for followers to sync with ZooKeeper |
| `max_client_connections` | `60` | Max client connections (0 = unlimited) |

### Auto-Purge Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `autopurge_snap_retain_count` | `3` | Number of snapshots to retain |
| `autopurge_purge_interval` | `1` | Purge interval in hours (0 = disable) |

### Resource Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `heap_size` | `256m` | JVM heap size |
| `log_level` | `INFO` | Log level (DEBUG, INFO, WARN, ERROR) |

## Environment Configuration

### Basic Setup

```yaml
# environment.yml
name: zookeeper-standalone
datacenter: local-docker

components:
  zookeeper:
    source: ./zookeeper
```

### Production Configuration

```yaml
# environment.yml
name: zookeeper-production
datacenter: aws-ecs

components:
  zookeeper:
    source: ./zookeeper
    variables:
      heap_size: "1g"
      max_client_connections: "200"
      log_level: "WARN"
```

## Monitoring

### Four Letter Words (4LW) Commands

ZooKeeper supports monitoring via "four letter words" sent to the client port:

```bash
# Check if server is running
echo ruok | nc <host> 2181
# Returns: imok

# Get server statistics
echo stat | nc <host> 2181

# Get configuration
echo conf | nc <host> 2181

# Get environment
echo envi | nc <host> 2181
```

### Admin Server

The admin server (port 8080) provides HTTP endpoints:
- `GET /commands` - List available commands
- `GET /commands/stat` - Server statistics
- `GET /commands/conf` - Configuration
- `GET /commands/ruok` - Health check

## Scaling Considerations

This component provides a single-node ZooKeeper deployment suitable for:
- Development environments
- Single-datacenter deployments
- Applications that can tolerate brief unavailability

For production high-availability, consider:
- Running a 3-node or 5-node ensemble
- Placing nodes across availability zones
- Using dedicated storage with low latency

## Troubleshooting

### Connection Issues

1. Verify the service is healthy using the `ruok` command
2. Check firewall rules for port 2181
3. Verify client connection limit hasn't been reached

### Performance Issues

1. Increase heap size for larger datasets
2. Use SSD storage for transaction logs
3. Monitor garbage collection with JVM flags

## Documentation

- [ZooKeeper Documentation](https://zookeeper.apache.org/doc/current/)
- [Administrator's Guide](https://zookeeper.apache.org/doc/current/zookeeperAdmin.html)
- [Programmer's Guide](https://zookeeper.apache.org/doc/current/zookeeperProgrammers.html)
