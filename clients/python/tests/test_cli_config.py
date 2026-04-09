import os
import sys

import pytest

import agentcoopdb.cli.config as cfg_mod
from agentcoopdb.cli.config import CLIConfig, load


@pytest.fixture()
def tmp_config(tmp_path, monkeypatch):
    """Redirect config_path() to a file inside tmp_path."""
    config_file = tmp_path / "config.toml"
    monkeypatch.setattr(cfg_mod, "config_path", lambda: config_file)
    return config_file


def test_write_creates_file(tmp_config):
    CLIConfig(base_url="http://localhost:8080", api_key="acd_dev_x").write()
    assert tmp_config.exists()


@pytest.mark.skipif(sys.platform == "win32", reason="chmod not meaningful on Windows")
def test_write_file_mode(tmp_config):
    CLIConfig(base_url="http://localhost:8080", api_key="acd_dev_x").write()
    mode = oct(os.stat(tmp_config).st_mode & 0o777)
    assert mode == oct(0o600)


def test_write_content(tmp_config):
    CLIConfig(base_url="http://localhost:8080", api_key="acd_dev_abc").write()
    text = tmp_config.read_text()
    assert "http://localhost:8080" in text
    assert "acd_dev_abc" in text


def test_load_roundtrip(tmp_config):
    original = CLIConfig(base_url="https://db.example.com", api_key="acd_live_secret")
    original.write()
    loaded = load()
    assert loaded is not None
    assert loaded.base_url == original.base_url
    assert loaded.api_key == original.api_key


def test_load_missing_file(tmp_path, monkeypatch):
    monkeypatch.setattr(cfg_mod, "config_path", lambda: tmp_path / "nonexistent.toml")
    assert load() is None


def test_load_missing_key(tmp_config):
    tmp_config.write_text('base_url = "http://localhost:8080"\n', encoding="utf-8")
    assert load() is None
