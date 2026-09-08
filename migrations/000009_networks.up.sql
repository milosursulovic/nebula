CREATE TABLE networks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subnets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    network_id UUID NOT NULL REFERENCES networks (id),
    cidr       TEXT NOT NULL,
    gateway    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_subnets_network_id ON subnets (network_id);

CREATE TABLE ip_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subnet_id   UUID NOT NULL REFERENCES subnets (id),
    ip_address  INET NOT NULL,
    status      TEXT NOT NULL DEFAULT 'AVAILABLE'
                CHECK (status IN ('AVAILABLE', 'ALLOCATED', 'RESERVED')),
    instance_id UUID REFERENCES instances (id),
    UNIQUE (subnet_id, ip_address)
);

-- Partial index: only AVAILABLE rows are ever scanned by the allocation
-- query's ORDER BY ... FOR UPDATE SKIP LOCKED, so this is the index that
-- matters for allocation throughput.
CREATE INDEX idx_ip_addresses_available ON ip_addresses (subnet_id, ip_address)
    WHERE status = 'AVAILABLE';

CREATE INDEX idx_ip_addresses_instance_id ON ip_addresses (instance_id)
    WHERE instance_id IS NOT NULL;
