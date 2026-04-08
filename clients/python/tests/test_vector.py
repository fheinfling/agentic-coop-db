from agentcoopdb.vector import format_vector


def test_format_vector_empty():
    assert format_vector([]) == "[]"


def test_format_vector_single():
    assert format_vector([1.5]) == "[1.5]"


def test_format_vector_multiple():
    assert format_vector([1.0, 2.5, -3.0]) == "[1.0,2.5,-3.0]"


def test_format_vector_no_spaces():
    assert " " not in format_vector([0.1, 0.2])


def test_format_vector_integers():
    assert format_vector([1, 2, 3]) == "[1.0,2.0,3.0]"
