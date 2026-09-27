import unittest

from windows_tests import batches, load_timings, partition


class ShardCoverageTests(unittest.TestCase):
    def test_every_root_test_appears_in_exactly_one_shard(self):
        names = ['Test' + str(index) for index in range(661)] + ['ExamplePage', 'FuzzParser']
        for count in (2, 4):
            with self.subTest(count=count):
                shards = [partition(names, index, count) for index in range(count)]
                for left in range(count):
                    for right in range(left + 1, count):
                        self.assertFalse(set(shards[left]) & set(shards[right]))
                self.assertCountEqual(sum(shards, []), names)
                self.assertEqual(partition(list(reversed(names)), 0, count), shards[0])

    def test_weighted_partition_balances_long_tests(self):
        names = ['TestSlowA', 'TestSlowB', 'TestFastA', 'TestFastB']
        timings = {'TestSlowA': 10, 'TestSlowB': 9, 'TestFastA': 1, 'TestFastB': 1}
        shards = [partition(names, index, 2, timings) for index in range(2)]
        self.assertNotEqual(
            next(index for index, shard in enumerate(shards) if 'TestSlowA' in shard),
            next(index for index, shard in enumerate(shards) if 'TestSlowB' in shard),
        )
        self.assertCountEqual(sum(shards, []), names)

    def test_unknown_tests_receive_heaviest_known_weight(self):
        names = ['TestKnown', 'TestUnknownA', 'TestUnknownB']
        shards = [partition(names, index, 2, {'TestKnown': 8}) for index in range(2)]
        self.assertCountEqual(sum(shards, []), names)

    def test_bad_shard_and_duplicate_discovery_fail(self):
        for names, shard, count in [(['TestA'], 2, 2), (['TestA'], 0, 0),
                                     (['TestA', 'TestA'], 0, 2)]:
            with self.subTest(names=names, shard=shard, count=count), self.assertRaises(ValueError):
                partition(names, shard, count)

    def test_process_batches_cover_each_root_and_its_subtests(self):
        names = ['Test' + str(index) for index in range(85)]
        groups = list(batches(names, 10))
        self.assertEqual([len(group) for group in groups], [10] * 8 + [5])
        self.assertEqual(sum(groups, []), names)
        self.assertEqual(list(batches([], 10)), [])
        with self.assertRaises(ValueError):
            list(batches(names, 0))


if __name__ == '__main__':
    unittest.main()
