<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/sqlite');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'sqlite3':
        $db = new SQLite3(':memory:');
        $db->exec('CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL)');
        $st = $db->prepare('INSERT INTO t (name) VALUES (:n)');
        foreach (['a', 'b', 'c'] as $n) {
            $st->bindValue(':n', $n, SQLITE3_TEXT);
            $st->execute();
        }
        $res = $db->query('SELECT id, name FROM t ORDER BY id');
        $out = [];
        while ($row = $res->fetchArray(SQLITE3_ASSOC)) {
            $out[] = $row['id'] . '=' . $row['name'];
        }
        echo implode(',', $out), ';', $db->querySingle('SELECT count(*) FROM t'), ';', $db->lastInsertRowID();
        break;
    case 'pdo_transaction':
        $db = new PDO('sqlite::memory:', null, null, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
        $db->exec('CREATE TABLE t (n INTEGER)');
        $db->beginTransaction();
        $db->exec('INSERT INTO t VALUES (1)');
        $db->rollBack();
        echo $db->query('SELECT count(*) FROM t')->fetchColumn();
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
