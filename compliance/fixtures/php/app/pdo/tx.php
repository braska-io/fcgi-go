<?php
// Transactions whose outcome a later request observes through obs.php.
//   op=commit&k=K       beginTransaction, insert, commit
//   op=open&k=K         beginTransaction, insert, end the script without commit
//   op=manual_open&k=K  exec('BEGIN'), insert, end the script without commit
//   op=uncaught&k=K     beginTransaction, insert, then an uncaught PDOException
//   op=exit&k=K         beginTransaction, insert, then exit()
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/tx');
$k = $q['k'] ?? '';
$db = app_db();

switch ($q['op'] ?? '') {
case 'commit':
    $db->beginTransaction();
    insert_ev($db, $k);
    $db->commit();
    echo 'committed';
    break;
case 'open':
    $db->beginTransaction();
    insert_ev($db, $k);
    echo 'left open';
    break;
case 'manual_open':
    $db->exec('BEGIN');
    insert_ev($db, $k);
    echo 'left open';
    break;
case 'uncaught':
    $db->beginTransaction();
    insert_ev($db, $k);
    echo 'before';
    $db->query('SELECT * FROM no_such_table');
    echo 'after';
    break;
case 'exit':
    $db->beginTransaction();
    insert_ev($db, $k);
    exit('exited');
default:
    http_response_code(400);
    echo 'unknown op';
}
