<?php
// Content-Type and Content-Length as both the CGI variable and the HTTP_*
// variable; frameworks that detect JSON read either one. null when unset.
require __DIR__ . '/_json.php';
fixture('app/compat/env');
emit_json([
    'content_type' => $_SERVER['CONTENT_TYPE'] ?? null,
    'http_content_type' => $_SERVER['HTTP_CONTENT_TYPE'] ?? null,
    'content_length' => $_SERVER['CONTENT_LENGTH'] ?? null,
    'http_content_length' => $_SERVER['HTTP_CONTENT_LENGTH'] ?? null,
]);
