<?php
// Status codes set the ways applications set them.
//   case=protocol  header('HTTP/1.0 406 Not Acceptable ') with a trailing space, then exit
//   case=code      http_response_code(401), no body
//   case=location  only an absolute Location
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/compat/status');

switch ($q['case'] ?? '') {
case 'protocol':
    header('HTTP/1.0 406 Not Acceptable ');
    exit;
case 'code':
    http_response_code(401);
    exit;
case 'location':
    header('Location: https://example.test/next');
    exit;
default:
    http_response_code(400);
    echo 'unknown case';
}
