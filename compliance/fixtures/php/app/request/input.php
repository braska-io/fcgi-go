<?php
// The raw body through php://input next to what PHP parsed from it.
// Bodies above 1 KiB are reported by length and digest only.
require __DIR__ . '/_emit.php';
fixture('app/request/input');
$raw = file_get_contents('php://input');
emit([
    'method' => $_SERVER['REQUEST_METHOD'] ?? null,
    'content_type' => $_SERVER['CONTENT_TYPE'] ?? null,
    'content_length' => $_SERVER['CONTENT_LENGTH'] ?? null,
    'post' => (object) $_POST,
    'files' => count($_FILES),
    'input_length' => strlen($raw),
    'input_sha256' => hash('sha256', $raw),
    'input' => strlen($raw) <= 1024 ? $raw : null,
    'warning' => last_warning(),
]);
