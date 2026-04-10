from __future__ import annotations


def test_root_whoami(client):
    r = client.get("/")
    assert r.status_code == 200
    data = r.json()
    assert data.get("service") == "ms_payments"
    assert "/api/v1/webhook/payment" in data.get("yookassa_webhook_post", "")


def test_health(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"ok": True}
