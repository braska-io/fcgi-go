<?php
// Response status through http_response_code(), header() and Status:.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/code');

switch ($q['case'] ?? 'get') {
    case 'get':
        var_dump(http_response_code());
        break;
    case 'set':
        var_dump(http_response_code(404));
        var_dump(http_response_code());
        break;
    case 'code':
        http_response_code((int) $q['code']);
        break;
    case 'http_line':
        header('HTTP/1.1 404 Not Found');
        break;
    case 'header_param':
        header('X-Code: 503', true, 503);
        break;
    case 'header_param_empty':
        // response_code only takes effect with a non-empty header.
        header('', true, 503);
        break;
    case 'status_header':
        header('Status: 404 Not Found');
        break;
}
