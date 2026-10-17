<?php
// Shared by the request fixtures: one stable JSON shape so cases can assert
// exact bodies. Not a fixture itself; the leading underscore keeps it out of
// any target.

require_once __DIR__ . '/../../_fixture.php';

function emit(mixed $data): void
{
    header('Content-Type: application/json');
    echo json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE);
}

// last_warning is the message of the last error PHP raised, including the
// ones raised while it parsed the request before the script started.
function last_warning(): ?string
{
    return error_get_last()['message'] ?? null;
}

// upload_entries flattens $_FILES into one entry per uploaded part, keyed by
// the field path ("f", "f[0]", "f[a][b]"). tmp_name is replaced by facts that
// do not change between runs.
function upload_entries(): array
{
    $out = [];
    foreach ($_FILES as $field => $info) {
        walk_upload($out, $field, $info);
    }
    return $out;
}

function walk_upload(array &$out, string $path, array $info): void
{
    if (is_array($info['name'])) {
        foreach (array_keys($info['name']) as $k) {
            $sub = [];
            foreach ($info as $key => $values) {
                $sub[$key] = $values[$k];
            }
            walk_upload($out, $path . '[' . $k . ']', $sub);
        }
        return;
    }
    $tmp = $info['tmp_name'];
    $ok = $info['error'] === UPLOAD_ERR_OK && $tmp !== '';
    $out[$path] = [
        'name' => $info['name'],
        'full_path' => $info['full_path'] ?? null,
        'type' => $info['type'],
        'size' => $info['size'],
        'error' => $info['error'],
        'uploaded' => $ok && is_uploaded_file($tmp),
        'sha256' => $ok ? hash_file('sha256', $tmp) : null,
    ];
}
