<?php
// An uncaught PDOException ends the script.
//   op=query    a query against a missing table, uncaught
//   op=connect  a DSN whose file cannot be opened, uncaught; the DSN is
//               15 bytes so the default trace argument limit keeps it whole
//   op=handler  the same query with set_exception_handler() answering 500
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/uncaught');

switch ($q['op'] ?? '') {
case 'query':
    $db = app_db();
    echo 'before';
    $db->query('SELECT * FROM no_such_table');
    echo 'after';
    break;
case 'connect':
    echo 'before';
    new PDO('sqlite:/no/x.db');
    echo 'after';
    break;
case 'handler':
    set_exception_handler(function (Throwable $e) {
        http_response_code(500);
        echo 'handled:' . get_class($e);
    });
    $db = app_db();
    $db->query('SELECT * FROM no_such_table');
    echo 'after';
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
