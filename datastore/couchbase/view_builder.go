package couchbase

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"strconv"
	"strings"
	"time"

	cb "github.com/couchbaselabs/go-couchbase"
	"github.com/couchbaselabs/query/datastore"
	"github.com/couchbaselabs/query/expression"
	"github.com/couchbaselabs/query/expression/parser"
	"github.com/couchbaselabs/query/logging"
)

type ddocJSON struct {
	cb.DDoc
	IndexOn       []string `json:"indexOn"`
	IndexChecksum int      `json:"indexChecksum"`
}

func newViewIndex(name string, on datastore.IndexKey, where expression.Expression, view *viewIndexer) (*viewIndex, error) {

	doc, err := newDesignDoc(name, view.keyspace.Name(), on, where)
	if err != nil {
		return nil, err
	}

	inst := viewIndex{
		name:     name,
		using:    datastore.VIEW,
		on:       on,
		where:    where,
		ddoc:     doc,
		view:     view,
		keyspace: view.keyspace,
	}

	logging.Infof("Created index %s on %s with key %v on where %v", name, view.keyspace.Name(), on, where)

	err = inst.putDesignDoc()
	if err != nil {
		return nil, err
	}

	err = inst.WaitForIndex()
	if err != nil {
		return nil, err
	}

	return &inst, nil
}

func (vi *viewIndex) String() string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("name: %v ", vi.name))
	buf.WriteString(fmt.Sprintf("on: %v ", vi.on))
	buf.WriteString(fmt.Sprintf("where: %v", vi.where))
	buf.WriteString(fmt.Sprintf("using: %v ", vi.using))
	buf.WriteString(fmt.Sprintf("ddoc: %v ", *vi.ddoc))
	buf.WriteString(fmt.Sprintf("bucket: %v ", *vi.keyspace))
	return buf.String()
}

func newDesignDoc(idxname string, bucketName string, on datastore.IndexKey, where expression.Expression) (*designdoc, error) {
	var doc designdoc

	doc.name = "ddl_" + idxname
	doc.viewname = idxname

	err := generateMap(bucketName, on, where, &doc)
	if err != nil {
		return nil, err
	}

	err = generateReduce(on, &doc)
	if err != nil {
		return nil, err
	}

	return &doc, nil
}

func loadViewIndexes(v *viewIndexer) ([]*datastore.Index, error) {

	b := v.keyspace
	rows, err := b.cbbucket.GetDDocs()
	if err != nil {
		return nil, err
	}

	inames := make([]string, 0, len(rows.Rows))
	nonUsableIndexes := make([]string, 0)

	for _, row := range rows.Rows {
		cdoc := row.DDoc
		id := cdoc.Meta["id"].(string)
		if strings.HasPrefix(id, "_design/ddl_") {
			iname := strings.TrimPrefix(id, "_design/ddl_")
			inames = append(inames, iname)
		} else if strings.HasPrefix(id, "_design/dev_") {
			// append this to the list of non-usuable indexes
			iname := strings.TrimPrefix(id, "_design/dev_")
			for _, name := range v.nonUsableIndexes {
				if iname == name {
					continue
				}
			}
			nonUsableIndexes = append(nonUsableIndexes, iname)

		} else if strings.HasPrefix(id, "_design/") {
			iname := strings.TrimPrefix(id, "_design/")
			for _, name := range v.nonUsableIndexes {
				if iname == name {
					continue
				}
			}
			nonUsableIndexes = append(nonUsableIndexes, iname)
		}

	}

	indexes := make([]*datastore.Index, 0, len(inames))
	for _, iname := range inames {
		ddname := "ddl_" + iname
		jdoc, err := getDesignDoc(b, ddname)
		if err != nil {
			return nil, err
		}
		jview, ok := jdoc.Views[iname]
		if !ok {
			nonUsableIndexes = append(nonUsableIndexes, iname)
			logging.Errorf("Missing view for index %v ", iname)
			continue
		}

		exprlist := make([]expression.Expression, 0, len(jdoc.IndexOn))

		for _, ser := range jdoc.IndexOn {
			if iname == PRIMARY_INDEX {
				doc := expression.NewIdentifier(b.Name())
				meta := expression.NewMeta(doc)
				mdid := expression.NewField(meta, expression.NewFieldName("id"))
				exprlist = append(exprlist, mdid)
			} else {
				expr, err := parser.Parse(ser)
				if err != nil {
					nonUsableIndexes = append(nonUsableIndexes, iname)
					logging.Errorf("Cannot unmarshal expression for index  %v", iname)
					continue
				}
				exprlist = append(exprlist, expr)
			}
		}
		if len(exprlist) != len(jdoc.IndexOn) {
			continue
		}

		ddoc := designdoc{
			name:     ddname,
			viewname: iname,
			mapfn:    jview.Map,
			reducefn: jview.Reduce,
		}
		if ddoc.checksum() != jdoc.IndexChecksum {
			nonUsableIndexes = append(nonUsableIndexes, iname)
			logging.Errorf("Warning - checksum failed on index  %v", iname)
			continue
		}

		var index datastore.Index

		logging.Infof("Found index name %v keyspace %v", iname, b.Name())
		if iname == PRIMARY_INDEX {
			index = &viewIndex{
				name:     iname,
				keyspace: b,
				view:     v,
				using:    datastore.VIEW,
				ddoc:     &ddoc,
				on:       exprlist,
			}
			indexes = append(indexes, &index)
		} else {
			index = &viewIndex{
				name:     iname,
				keyspace: b,
				view:     v,
				using:    datastore.VIEW,
				ddoc:     &ddoc,
				on:       exprlist,
			}
			indexes = append(indexes, &index)
		}
	}
	v.nonUsableIndexes = nonUsableIndexes

	if len(indexes) == 0 {
		return nil, nil
	}

	return indexes, nil
}

func newViewPrimaryIndex(v *viewIndexer, name string) (*primaryIndex, error) {
	ddoc := newPrimaryDDoc(name)
	doc := expression.NewIdentifier(v.keyspace.Name())
	meta := expression.NewMeta(doc)
	mdid := expression.NewField(meta, expression.NewFieldName("id"))

	inst := primaryIndex{
		viewIndex{
			name:     name,
			using:    datastore.VIEW,
			on:       datastore.IndexKey{mdid},
			ddoc:     ddoc,
			keyspace: v.keyspace,
			view:     v,
		},
	}

	err := inst.putDesignDoc()
	if err != nil {
		return nil, err
	}

	err = inst.WaitForIndex()
	if err != nil {
		return nil, err
	}

	return &inst, nil
}

func newPrimaryDDoc(name string) *designdoc {
	var doc designdoc
	line := strings.Replace(templPrimary, "$rnd", strconv.Itoa(int(rand.Int31())), -1)
	line = strings.Replace(line, "$string", strconv.Itoa(TYPE_STRING), -1)
	doc.mapfn = line
	doc.reducefn = ""
	doc.name = "ddl_" + name
	doc.viewname = name
	return &doc
}

func generateMap(bucketName string, on datastore.IndexKey, where expression.Expression, doc *designdoc) error {

	buf := new(bytes.Buffer)

	fmt.Fprintln(buf, templStart)
	line := strings.Replace(templFunctions, "$null", strconv.Itoa(TYPE_NULL), -1)
	line = strings.Replace(line, "$boolean", strconv.Itoa(TYPE_BOOLEAN), -1)
	line = strings.Replace(line, "$number", strconv.Itoa(TYPE_NUMBER), -1)
	line = strings.Replace(line, "$string", strconv.Itoa(TYPE_STRING), -1)
	line = strings.Replace(line, "$array", strconv.Itoa(TYPE_ARRAY), -1)
	line = strings.Replace(line, "$object", strconv.Itoa(TYPE_OBJECT), -1)
	fmt.Fprintln(buf, line)

	keylist := new(bytes.Buffer)

	for idx, expr := range on {

		walker := NewWalker()
		_, err := walker.Visit(expr)
		if err != nil {
			return err
		}

		jvar := fmt.Sprintf("key%v", idx+1)
		line := strings.Replace(templExpr, "$var", jvar, -1)
		line = strings.Replace(line, "$path", walker.JS(), -1)
		fmt.Fprint(buf, line)

		if idx > 0 {
			fmt.Fprint(keylist, ", ")
		}
		fmt.Fprint(keylist, jvar)
	}

	line = strings.Replace(templKey, "$keylist", keylist.String(), -1)

	fmt.Fprint(buf, line)

	var whereCondition string
	if where != nil {

		walker := NewWalker()
		_, err := walker.VisitWhere(bucketName, where)
		if err != nil {
			return err
		}

		whereCondition = walker.JS()

	}
	if whereCondition != "" {
		line := strings.Replace(tmplWhere, "$wherecondition", whereCondition, 1)
		fmt.Fprintf(buf, line)

	} else {
		fmt.Fprint(buf, templEmit)
	}

	line = strings.Replace(templEnd, "$rnd", strconv.Itoa(int(rand.Int31())), -1)
	fmt.Fprint(buf, line)

	doc.mapfn = buf.String()
	// debug
	//fmt.Printf(doc.mapfn)
	return nil
}

func generateReduce(on datastore.IndexKey, doc *designdoc) error {
	doc.reducefn = ""
	return nil
}

func (idx *viewIndex) putDesignDoc() error {
	var view cb.ViewDefinition
	view.Map = idx.ddoc.mapfn

	var put ddocJSON
	put.Views = make(map[string]cb.ViewDefinition)
	put.Views[idx.name] = view
	put.IndexChecksum = idx.ddoc.checksum()

	put.IndexOn = make([]string, len(idx.on))
	for idx, expr := range idx.on {
		put.IndexOn[idx] = expression.NewStringer().Visit(expr)
	}

	if err := idx.keyspace.cbbucket.PutDDoc(idx.DDocName(), &put); err != nil {
		return err
	}

	var saved *ddocJSON = nil
	var err error = nil

	// give the PUT some time to register
	for i := 0; i < 3; i++ {
		if i > 1 {
			time.Sleep(time.Duration(i*3) * time.Second)
		}

		saved, err = getDesignDoc(idx.keyspace, idx.DDocName())
		if err == nil {
			break
		}
	}

	if err != nil {
		return errors.New("Creating index '" + idx.name + "' failed: " + err.Error())
	}

	if saved.IndexChecksum != idx.ddoc.checksum() {
		return errors.New("Checksum mismatch after creating index '" + idx.name + "'")
	}

	return nil
}

func (ddoc *designdoc) checksum() int {
	mapSum := crc32.ChecksumIEEE([]byte(ddoc.mapfn))
	reduceSum := crc32.ChecksumIEEE([]byte(ddoc.reducefn))
	return int(mapSum + reduceSum)
}

func getDesignDoc(b *keyspace, ddocname string) (*ddocJSON, error) {
	var ddoc ddocJSON
	err := b.cbbucket.GetDDoc(ddocname, &ddoc)
	if err != nil {
		return nil, err
	}
	return &ddoc, nil
}

func (idx *viewIndex) DropViewIndex() error {
	if err := idx.keyspace.cbbucket.DeleteDDoc(idx.ddoc.name); err != nil {
		return err
	}
	return nil
}

func (idx *viewIndex) WaitForIndex() error {
	var err error
	// if we have got this far, very likely any errors are
	// due to index not yet being noticed by the system.
	for i := 0; i < 3; i++ {
		if i > 1 {
			time.Sleep(time.Duration(i*3) * time.Second)
		}
		_, err = idx.keyspace.cbbucket.View(
			idx.ddoc.name,
			idx.ddoc.viewname,
			map[string]interface{}{
				"start_key": []interface{}{"thing"},
				"end_key":   []interface{}{"thing", map[string]string{}},
				"stale":     false,
			})
		if err == nil {
			break
		}
	}
	return err
}

// AST to JS conversion
type JsStatement struct {
	js bytes.Buffer
}

func NewWalker() *JsStatement {
	var js JsStatement
	return &js
}

func (this *JsStatement) JS() string {
	return this.js.String()
}

// inorder traversal of the AST to get JS expression out of it
func (this *JsStatement) Visit(e expression.Expression) (expression.Expression, error) {

	this.js.WriteString("doc.")
	stringer := NewJSConverter().Visit(e)
	if stringer != "" {
		stringer = strings.Replace(stringer, "`", "", -1)
		this.js.WriteString(stringer)
	} else {
		return e, errors.New("This Expression is not supported by indexing")
	}

	return e, nil
}

// inorder traversal of the where expression AST to get JS expression out of it
func (this *JsStatement) VisitWhere(bucketName string, e expression.Expression) (expression.Expression, error) {

	stringer := NewJSConverter().Visit(e)
	if stringer != "" {
		stringer = strings.Replace(stringer, "`", "", -1)
		// replace all instances of bucket-name with doc.bucket-name
		stringer = strings.Replace(stringer, bucketName, "doc", -1)
		this.js.WriteString(stringer)
	} else {
		return e, errors.New("This Expression is not supported by indexing")
	}

	return e, nil
}
