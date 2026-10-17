<?php
// Shared by the compat fixtures: one JSON shape with fixed key order, so
// cases can assert exact bodies. Not a fixture itself.
require_once __DIR__ . '/../../_fixture.php';

function emit_json(array $data): void
{
    header('Content-Type: application/json');
    echo json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE);
}

// marker_path is the file a compat fixture leaves under $FIXTURE_STATE to
// show that code ran after the response was produced.
function marker_path(string $k): string
{
    return getenv('FIXTURE_STATE') . '/compat-' . preg_replace('/[^a-z0-9-]/', '_', $k);
}
