<?php
// Stateless PDO behaviour: binding, fetch modes, error modes, lastInsertId,
// in-request transactions. Every op works on TEMP tables of a fresh,
// non-persistent connection to $FIXTURE_STATE/app.db, so nothing outlives
// the request. Each op writes one short line the catalog compares exactly.
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/_db.php';
$q = fixture('app/pdo/basic');

$db = app_db();
$db->exec('CREATE TEMP TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, grp TEXT)');
$db->exec("INSERT INTO t (name, grp) VALUES ('a', 'x'), ('b', 'y'), ('c', 'x')");

function typeof_bound(PDO $db, mixed $value, ?int $type): string
{
    $st = $db->prepare('SELECT typeof(?)');
    if ($type === null) {
        $st->bindValue(1, $value);
    } else {
        $st->bindValue(1, $value, $type);
    }
    $st->execute();
    return $st->fetchColumn();
}

class Row
{
    public $id;
    public $name;
    public string $seen;

    public function __construct()
    {
        // FETCH_CLASS fills properties before the constructor runs.
        $this->seen = $this->name === null ? 'post' : 'pre:' . $this->name;
    }
}

$sel = 'SELECT id, name FROM t ORDER BY id';

switch ($q['op'] ?? '') {
// --------------------------------------------------------------- connect
case 'driver':
    echo $db->getAttribute(PDO::ATTR_DRIVER_NAME);
    break;
case 'errmode_default':
    $plain = new PDO('sqlite::memory:');
    echo $plain->getAttribute(PDO::ATTR_ERRMODE) === PDO::ERRMODE_EXCEPTION ? 'exception' : 'other';
    break;
case 'connect_fail_silent':
    try {
        new PDO('sqlite:/nonexistent-dir/x.db', null, null, [PDO::ATTR_ERRMODE => PDO::ERRMODE_SILENT]);
        echo 'no exception';
    } catch (PDOException $e) {
        echo 'PDOException';
    }
    break;
case 'close_null':
    // A persistent-free handle closes when the last reference goes away;
    // a TEMP table of the closed connection is gone on the next one.
    $db = null;
    $db = app_db();
    echo $db->query("SELECT COUNT(*) FROM sqlite_temp_master WHERE name = 't'")->fetchColumn();
    break;

// --------------------------------------------------------------- binding
case 'bind_int':
    echo typeof_bound($db, 42, PDO::PARAM_INT);
    break;
case 'bind_str':
    echo typeof_bound($db, 42, PDO::PARAM_STR);
    break;
case 'bind_default':
    echo typeof_bound($db, 42, null);
    break;
case 'bind_null':
    echo typeof_bound($db, null, PDO::PARAM_NULL);
    break;
case 'bind_bool':
    echo typeof_bound($db, true, PDO::PARAM_BOOL);
    break;
case 'bind_lob':
    $st = $db->prepare('SELECT typeof(?1) || \':\' || length(?1)');
    $st->bindValue(1, "\x00\x01\x02", PDO::PARAM_LOB);
    $st->execute();
    echo $st->fetchColumn();
    break;
case 'execute_array':
    $st = $db->prepare('SELECT typeof(?)');
    $st->execute([42]);
    echo $st->fetchColumn();
    break;
case 'named':
    $st = $db->prepare('SELECT name FROM t WHERE grp = :g AND id > :min ORDER BY id');
    $st->execute([':g' => 'x', ':min' => 1]);
    echo implode(',', $st->fetchAll(PDO::FETCH_COLUMN));
    break;
case 'bind_param_reference':
    $st = $db->prepare('SELECT name FROM t WHERE id = ?');
    $id = 1;
    $st->bindParam(1, $id, PDO::PARAM_INT);
    $id = 3; // bindParam evaluates the variable at execute()
    $st->execute();
    echo $st->fetchColumn();
    break;
case 'injection':
    $evil = "x'); DROP TABLE t; --";
    $db->prepare('INSERT INTO t (name, grp) VALUES (?, ?)')->execute([$evil, 'z']);
    $st = $db->prepare('SELECT COUNT(*) FROM t WHERE name = ?');
    $st->execute([$evil]);
    echo $st->fetchColumn() . ':' . $db->query('SELECT COUNT(*) FROM t')->fetchColumn();
    break;
case 'native_types':
    $row = $db->query("SELECT 1 AS i, 1.5 AS r, 'x' AS s, NULL AS n")->fetch(PDO::FETCH_ASSOC);
    echo implode(',', array_map('get_debug_type', $row));
    break;
case 'stringify':
    $db->setAttribute(PDO::ATTR_STRINGIFY_FETCHES, true);
    $row = $db->query('SELECT 1 AS i, NULL AS n')->fetch(PDO::FETCH_ASSOC);
    echo implode(',', array_map('get_debug_type', $row));
    break;

// ----------------------------------------------------------- fetch modes
case 'fetch_assoc':
    echo json_encode($db->query($sel)->fetch(PDO::FETCH_ASSOC));
    break;
case 'fetch_num':
    echo json_encode($db->query($sel)->fetch(PDO::FETCH_NUM));
    break;
case 'fetch_both_default':
    $keys = array_map('strval', array_keys($db->query($sel)->fetch()));
    sort($keys, SORT_STRING);
    echo implode(',', $keys);
    break;
case 'fetch_obj':
    $o = $db->query($sel)->fetch(PDO::FETCH_OBJ);
    echo get_class($o) . ':' . $o->name;
    break;
case 'fetch_class':
    $st = $db->query($sel);
    $st->setFetchMode(PDO::FETCH_CLASS, Row::class);
    $o = $st->fetch();
    echo get_class($o) . ':' . $o->seen;
    break;
case 'fetch_named':
    echo json_encode($db->query('SELECT 1 AS x, 2 AS x')->fetch(PDO::FETCH_NAMED));
    break;
case 'fetch_column':
    echo implode(',', $db->query($sel)->fetchAll(PDO::FETCH_COLUMN, 1));
    break;
case 'fetch_column_missing':
    try {
        $db->query($sel)->fetchAll(PDO::FETCH_COLUMN, 9);
        echo 'no error';
    } catch (ValueError $e) {
        echo 'ValueError';
    }
    break;
case 'fetch_key_pair':
    echo json_encode($db->query($sel)->fetchAll(PDO::FETCH_KEY_PAIR));
    break;
case 'fetch_group':
    echo json_encode($db->query('SELECT grp, name FROM t ORDER BY id')->fetchAll(PDO::FETCH_GROUP | PDO::FETCH_COLUMN));
    break;
case 'fetch_unique':
    echo json_encode($db->query('SELECT id, name FROM t ORDER BY id')->fetchAll(PDO::FETCH_UNIQUE | PDO::FETCH_ASSOC));
    break;
case 'fetch_end':
    $st = $db->query('SELECT id FROM t WHERE id = 1');
    $st->fetch();
    echo var_export($st->fetch(), true);
    break;
case 'fetch_column_scalar':
    echo $db->query('SELECT COUNT(*) FROM t')->fetchColumn();
    break;

// ------------------------------------------------------------ error modes
case 'errmode_exception':
    try {
        $db->query('SELECT * FROM no_such_table');
        echo 'no exception';
    } catch (PDOException $e) {
        echo 'PDOException:' . $e->getCode();
    }
    break;
case 'errmode_silent':
    $db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_SILENT);
    $r = $db->query('SELECT * FROM no_such_table');
    $info = $db->errorInfo();
    echo var_export($r, true) . ':' . $db->errorCode() . ':' . (str_contains((string) $info[2], 'no such table') ? 'msg' : 'nomsg');
    break;
case 'errmode_warning':
    $db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_WARNING);
    $seen = 'none';
    set_error_handler(function (int $no) use (&$seen) {
        $seen = $no === E_WARNING ? 'E_WARNING' : 'other';
        return true;
    });
    $r = $db->query('SELECT * FROM no_such_table');
    restore_error_handler();
    echo var_export($r, true) . ':' . $seen;
    break;

// ----------------------------------------------------------- lastInsertId
case 'last_insert_id':
    $db->exec('CREATE TEMP TABLE li (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT)');
    $db->exec("INSERT INTO li (v) VALUES ('a')");
    $db->exec("INSERT INTO li (v) VALUES ('b')");
    $id = $db->lastInsertId();
    echo get_debug_type($id) . ':' . $id;
    break;
case 'last_insert_id_explicit':
    $db->exec('CREATE TEMP TABLE li (id INTEGER PRIMARY KEY, v TEXT)');
    $db->exec("INSERT INTO li (id, v) VALUES (100, 'a')");
    echo $db->lastInsertId();
    break;

// ----------------------------------------------- transactions, in-request
case 'tx_commit':
    $during = $db->beginTransaction() && $db->inTransaction();
    $db->exec("INSERT INTO t (name, grp) VALUES ('d', 'z')");
    $db->commit();
    echo var_export($during, true) . ':' . var_export($db->inTransaction(), true) . ':'
        . $db->query("SELECT COUNT(*) FROM t WHERE name = 'd'")->fetchColumn();
    break;
case 'tx_rollback':
    $db->beginTransaction();
    $db->exec("INSERT INTO t (name, grp) VALUES ('d', 'z')");
    $db->rollBack();
    echo var_export($db->inTransaction(), true) . ':'
        . $db->query("SELECT COUNT(*) FROM t WHERE name = 'd'")->fetchColumn();
    break;
case 'tx_nested':
    $db->beginTransaction();
    try {
        $db->beginTransaction();
        echo 'no exception';
    } catch (PDOException $e) {
        echo 'PDOException';
    }
    break;
case 'tx_nested_silent':
    $db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_SILENT);
    $db->beginTransaction();
    try {
        $db->beginTransaction();
        echo 'no exception';
    } catch (PDOException $e) {
        echo 'PDOException';
    }
    break;
case 'commit_without_tx':
    try {
        $db->commit();
        echo 'no exception';
    } catch (PDOException $e) {
        echo 'PDOException';
    }
    break;
case 'savepoint_rollback_to':
    $db->beginTransaction();
    $db->exec("INSERT INTO t (name, grp) VALUES ('s1', 'sp')");
    $db->exec('SAVEPOINT p');
    $db->exec("INSERT INTO t (name, grp) VALUES ('s2', 'sp')");
    $db->exec('ROLLBACK TO p');
    $db->exec("INSERT INTO t (name, grp) VALUES ('s3', 'sp')");
    $db->commit();
    echo implode(',', $db->query("SELECT name FROM t WHERE grp = 'sp' ORDER BY id")->fetchAll(PDO::FETCH_COLUMN));
    break;
case 'savepoint_release':
    $db->beginTransaction();
    $db->exec('SAVEPOINT p');
    $db->exec("INSERT INTO t (name, grp) VALUES ('s1', 'sp')");
    $db->exec('RELEASE p');
    $still = var_export($db->inTransaction(), true);
    $db->rollBack();
    echo $still . ':' . $db->query("SELECT COUNT(*) FROM t WHERE grp = 'sp'")->fetchColumn();
    break;
default:
    http_response_code(400);
    echo 'unknown op';
}
